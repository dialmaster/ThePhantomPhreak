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
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	irc "github.com/thoj/go-ircevent"
)

const (
	chatgptURL = "https://api.openai.com/v1/chat/completions"
)

type conf struct {
	ApiKey                               string `yaml:"ApiKey"`
	BotContext                           string `yaml:"BotContext"`
	MemorySize                           int    `yaml:"MemorySize"`
	IrcServer                            string `yaml:"IrcServer"`
	IrcPort                              string `yaml:"IrcPort"`
	ChatRoom                             string `yaml:"ChatRoom"`
	BotName                              string `yaml:"BotName"`
	OpenAIOrganization                   string `yaml:"OpenAI-Organization"`
	ContextLinesForResponseDetermination int    `yaml:"ContextLinesForResponseDetermination"`
	ResponseGPTModel                     string `yaml:"ResponseGPTModel"`
	ShouldRespondGPTModel                string `yaml:"ShouldRespondGPTModel"`
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
var mutex sync.Mutex
var messagesSinceLastMessageWithoutBeingPrompted = 5

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
			c.getConf()
			// Clear the contents of prevMsgs
			prevMsgs = []string{}
			conn.Join(c.ChatRoom)
			conn.Privmsg(c.ChatRoom, "I was kicked from the room. My memory has been cleared.")
		}
	})
	conn.AddCallback("PRIVMSG", func(event *irc.Event) {
		eventCopy := *event

		go func() {
			checkMsgs := append(prevMsgs, eventCopy.Nick+": "+eventCopy.Message())
			shouldIRespond := shouldIRespond(checkMsgs)

			if shouldIRespond {
				mutex.Lock()
				prevMsgs = append(prevMsgs, eventCopy.Nick+": "+eventCopy.Message())
				mutex.Unlock()
				input := strings.Join(prevMsgs, "\n")
				input += "\n" + c.BotName + ": "
				fmt.Println("Input for response:", input)
				fmt.Println("")

				// More expensive, more accurate
				//response, err := chatgptResponse(c.BotContext, input, "gpt-4")

				// Less expensive
				response, err := chatgptResponse(c.BotContext, input, "gpt-3.5-turbo")
				if err != nil {
					fmt.Println("Error getting chatgpt response:", err)
					return
				}

				// Break response on line break
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
						// time.Sleep(time.Duration(rand.Intn(3)) * time.Second)

						// If line is > 384 characters, split it into multiple messages on word boundaries
						if len(line) > 384 {
							words := strings.Split(line, " ")
							var msg string
							for _, word := range words {
								if len(msg)+len(word) > 384 {
									conn.Privmsg(c.ChatRoom, msg)
									mutex.Lock()
									prevMsgs = append(prevMsgs, c.BotName+": "+msg)
									mutex.Unlock()
									msg = word + " "
								} else {
									msg += word + " "
								}
							}
							// Send the remaining part of the message, if there is any.
							if msg != "" {
								conn.Privmsg(c.ChatRoom, msg)
								mutex.Lock()
								prevMsgs = append(prevMsgs, c.BotName+": "+msg)
								mutex.Unlock()
							}
						} else {
							conn.Privmsg(c.ChatRoom, line)
							mutex.Lock()
							prevMsgs = append(prevMsgs, c.BotName+": "+line)
							mutex.Unlock()
						}
					}
				}

			} else {
				mutex.Lock()
				prevMsgs = append(prevMsgs, eventCopy.Nick+": "+eventCopy.Message())
				mutex.Unlock()
			}
			// if prevMgs is longer than memory buffer, remove the first element (limited memory)
			if len(prevMsgs) > c.MemorySize {
				mutex.Lock()
				prevMsgs = prevMsgs[1:]
				mutex.Unlock()
			}
		}()
	})
	conn.Loop()
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

/**
 * Should I respond to the last messages in the stream?
 * This will use the cheaper version of GPT-3.5 to determine if I should respond
 * @param input The input JSON string, containing the last 5 messages in the stream
 * @return true if I should respond, false otherwise
 */
func shouldIRespond(checkMsgs []string) bool {
	numMsgs := len(checkMsgs)
	numToJoin := c.ContextLinesForResponseDetermination
	if numMsgs < numToJoin {
		numToJoin = numMsgs
	}

	lastMsgs := strings.Join(checkMsgs[numMsgs-numToJoin:], "\n")
	fmt.Println("Checking to see if I should respond to:", lastMsgs)
	fmt.Println("")

	// Randomly, with a 1 in 10 change, the bot has a higher chance to respond to the conversation
	// Unless it has already had that happen in the last 5 messages...
	rand.Seed(time.Now().UnixNano())
	number := rand.Intn(10) + 1

	roleText := ""
	if number == 1 && messagesSinceLastMessageWithoutBeingPrompted > 5 {
		messagesSinceLastMessageWithoutBeingPrompted = 0
		roleText = `You are ` + c.BotName + `, a participant in a chat room.
		Based on the following series of messages, give me a boolean indicating if you SHOULD respond to the last message in the stream.
		If your name was mentioned, but it seems like you should not respond based on context, return false. Even if you are not mentioned, if it seems like the conversation is a topic you would talk about, return true.
		Give the a response in validJSON format like follows:
		{ "shouldRespond": boolean, "respondReason": string }

		shouldRespond is the boolean that indicates if ` + c.BotName + ` should respond.
		respondReason is the reasoning behind the boolean.`

	} else {
		messagesSinceLastMessageWithoutBeingPrompted += 1

		roleText = `You are ` + c.BotName + `, a participant in a chat room.
		Based on the following series of messages, give me a boolean indicating if you SHOULD respond to the last message in the stream.
		If your name was mentioned, but it seems like you should not respond based on context, return false.
		Give the a response in validJSON format like follows:
		{ "shouldRespond": boolean, "respondReason": string }

		shouldRespond is the boolean that indicates if ` + c.BotName + ` should respond.
		respondReason is the reasoning behind the boolean.`
	}

	jsonResponse, err1 := chatgptResponse(roleText, lastMsgs, c.ShouldRespondGPTModel)
	if err1 != nil {
		fmt.Println("Error getting chatgpt response in shouldIRespond:", err1)
		return false
	}

	// Debug print the jsonResponse variable
	fmt.Println("Should I respond?", jsonResponse)
	fmt.Println("")

	// Try to evaluate the jsonResponse variable as a JSON object
	var response map[string]interface{}

	err := json.Unmarshal([]byte(jsonResponse), &response)
	if err != nil {
		fmt.Println("Error in unmarshalling response for shouldIRespond")
		return false
	}

	return response["shouldRespond"].(bool)
}

// The api key in config.yml to grant.s.dial@gmail.com account and will expire on June 1st, 2023
func chatgptResponse(roleText string, input string, model string) (string, error) {
	data := map[string]interface{}{
		"model": c.ResponseGPTModel,
		"messages": []Message{
			{
				Role:    "system",
				Content: roleText,
			},
			{
				Role:    "user",
				Content: input,
			},
		},
	}

	payload, err := json.Marshal(data)

	if err != nil {
		fmt.Println("Error 1")
		return "", err
	}
	client := &http.Client{
		Timeout: 15 * time.Second,
	}
	req, err := http.NewRequest("POST", chatgptURL, bytes.NewBuffer(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.ApiKey)
	req.Header.Set("OpenAI-Organization", c.OpenAIOrganization)
	req.Header.Set("Content-Type", "application/json")

	var response struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int    `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("received non-200 response: %s", resp.Status)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Sometimes, randomly, chatGPT wraps the JSON in markdown, so we have to remove it
	bodyString := string(body)
	bodyString = strings.Replace(bodyString, "```json", "", -1)
	bodyString = strings.Replace(bodyString, "```", "", -1)
	body = []byte(bodyString)

	err = json.Unmarshal(body, &response)
	if err != nil {
		fmt.Println("Error in unmarshalling response:", err)
		return "", err
	}

	if len(response.Choices) == 0 || response.Choices[0].Message.Role != "assistant" {
		fmt.Println("Error: No assistant message in response")
		return "", fmt.Errorf("no response from chatgpt")
	}

	// Print debug info for response
	fmt.Printf("ChatGPT response: %+v\n", response)
	text := response.Choices[0].Message.Content

	return text, nil
}
