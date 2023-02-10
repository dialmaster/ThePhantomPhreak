package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	irc "github.com/thoj/go-ircevent"
)

const (
	chatgptURL = "https://api.openai.com/v1/completions"
)

type conf struct {
	ApiKey     string `yaml:"ApiKey"`
	BotContext string `yaml:"BotContext"`
	MemorySize int    `yaml:"MemorySize"`
	IrcServer  string `yaml:"IrcServer"`
	IrcPort    string `yaml:"IrcPort"`
	ChatRoom   string `yaml:"ChatRoom"`
	BotName    string `yaml:"BotName"`
}

func (c *conf) getConf() *conf {
	myConfigFile := "config.yml"
	if _, err := os.Stat("config.yml"); err == nil {
		myConfigFile = "config.yml"
	}

	yamlFile, err := ioutil.ReadFile(myConfigFile)
	if err != nil {
		log.Printf("yamlFile.Get err   #%v ", err)
	}
	err = yaml.Unmarshal(yamlFile, c)
	if err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}

	return c
}

var c conf
var prevMsgs []string

func main() {
	c.getConf()

	conn := irc.IRC(c.BotName, c.BotName)
	err := conn.Connect(c.IrcServer + ":" + c.IrcPort)
	conn.UseTLS = true
	conn.TLSConfig = &tls.Config{
		InsecureSkipVerify: true,
	}
	if err != nil {
		fmt.Println("Error connecting:", err)
		return
	}
	conn.AddCallback("001", func(event *irc.Event) {
		conn.Join(c.ChatRoom)
	})
	conn.AddCallback("KICK", func(event *irc.Event) {
		if event.Arguments[1] == c.BotName {
			// Wait 15 seconds before rejoining
			time.Sleep(5 * time.Second)
			// Clear the contents of prevMsgs
			prevMsgs = []string{}
			conn.Join(c.ChatRoom)
			conn.Privmsg(c.ChatRoom, "I was kicked from the room. My memory has been cleared.")
		}
	})
	conn.AddCallback("PRIVMSG", func(event *irc.Event) {
		if strings.HasPrefix(event.Message(), c.BotName+", ") || strings.HasPrefix(event.Message(), c.BotName+": ") || rand.Intn(30) == 0 {
			inputPrime := c.BotContext
			prevMsgs = append(prevMsgs, event.Nick+": "+event.Message())
			input := inputPrime + strings.Join(prevMsgs, "\n")
			input += "\n" + c.BotName + ": "
			// print debug info for input
			fmt.Println(input)
			response, err := chatgptResponse(input)
			if err != nil {
				fmt.Println("Error getting chatgpt response:", err)
				return
			}

			// Break response on line break
			// Test comment
			responses := strings.Split(response, "\n")

			for _, line := range responses {
				if line != "" {
					// Strip leading spaces from response
					line = strings.TrimLeft(line, " ")
					// If the line begins with the bot name, remove it
					if strings.HasPrefix(line, c.BotName+": ") {
						line = strings.TrimPrefix(line, c.BotName+": ")
					}

					// Add a random delay between 0 and 2 seconds before sending response
					time.Sleep(time.Duration(rand.Intn(3)) * time.Second)
					conn.Privmsg(c.ChatRoom, line)
					prevMsgs = append(prevMsgs, c.BotName+": "+line)
				}
			}

		} else {
			prevMsgs = append(prevMsgs, event.Nick+": "+event.Message())
		}
		// if prevMgs is longer than memory buffer, remove the first element (limited memory)
		if len(prevMsgs) > c.MemorySize {
			prevMsgs = prevMsgs[1:]
		}
	})
	conn.Loop()
}

// The api key in config.yml to grant.s.dial@gmail.com account and will expire on June 1st, 2023
func chatgptResponse(input string) (string, error) {
	data := map[string]interface{}{
		"prompt":      input,
		"max_tokens":  100,
		"temperature": 0.5,
		"model":       "text-davinci-003",
	}
	payload, err := json.Marshal(data)
	if err != nil {
		fmt.Println("Error 1")
		return "", err
	}
	client := &http.Client{}
	req, err := http.NewRequest("POST", chatgptURL, bytes.NewBuffer(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.ApiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var response struct {
		Choices []struct {
			Text string `json:"text"`
		} `json:"choices"`
	}
	err = json.Unmarshal(body, &response)
	if err != nil {
		fmt.Println("Error 4")
		return "", err
	}
	if len(response.Choices) == 0 {
		fmt.Println("Error 5")
		return "", fmt.Errorf("no response from chatgpt")
	}
	// Print debug info for response
	fmt.Printf("%+v\n", response)
	var text = response.Choices[0].Text
	fmt.Println(text)
	return response.Choices[0].Text, nil
}
