package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	irc "github.com/thoj/go-ircevent"
)

const (
	//chatgptURL = "https://api.openai.com/v1/engines/chat-davinci/messages"
	chatgptURL = "https://api.openai.com/v1/completions"
	prompt     = "ThePhantomPhreak: "
)

type conf struct {
	ApiKey string `yaml:"ApiKey"`
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

func main() {
	c.getConf()

	conn := irc.IRC("ThePhantomPhreak", "ThePhantomPhreak")
	err := conn.Connect("irc3.nerdbucket.com:6670")
	conn.UseTLS = true
	conn.TLSConfig = &tls.Config{
		InsecureSkipVerify: true,
	}
	if err != nil {
		fmt.Println("Error connecting:", err)
		return
	}
	conn.AddCallback("001", func(event *irc.Event) {
		conn.Join("#nogoodshits")
	})
	conn.AddCallback("PRIVMSG", func(event *irc.Event) {
		if strings.HasPrefix(event.Message(), prompt) {
			input := strings.TrimPrefix(event.Message(), prompt)
			response, err := chatgptResponse(input)
			if err != nil {
				fmt.Println("Error getting chatgpt response:", err)
				return
			}
			// Strip line breaks from response
			response = strings.ReplaceAll(response, "\n", "")
			conn.Privmsg("#nogoodshits", response)
		}
	})
	conn.Loop()
}

// The api key in config.yml to grant.s.dial@gmail.com account and will expire on June 1st, 2023
func chatgptResponse(input string) (string, error) {
	data := map[string]interface{}{
		"prompt":      input + " Write the response as if you were a snarky teenage hacker.",
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
	fmt.Printf("%+v\n", body)
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
	// Print debug info for response
	fmt.Printf("%+v\n", response)
	var text = response.Choices[0].Text
	fmt.Println(text)
	return response.Choices[0].Text, nil
}
