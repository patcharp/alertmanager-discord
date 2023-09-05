package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// Discord color values
const (
	ColorRed    = 0x992D22
	ColorOrange = 0xF0B816
	ColorGreen  = 0x2ECC71
	ColorGrey   = 0x95A5A6
	ColorBlue   = 0x58b9ff
)

type alertManAlert struct {
	Annotations struct {
		Description string `json:"description"`
		Summary     string `json:"summary"`
	} `json:"annotations"`
	EndsAt       string            `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Labels       map[string]string `json:"labels"`
	StartsAt     string            `json:"startsAt"`
	Status       string            `json:"status"`
}

type alertManOut struct {
	Alerts            []alertManAlert `json:"alerts"`
	CommonAnnotations struct {
		Summary string `json:"summary"`
	} `json:"commonAnnotations"`
	CommonLabels struct {
		Alertname string `json:"alertname"`
	} `json:"commonLabels"`
	ExternalURL string `json:"externalURL"`
	GroupKey    string `json:"groupKey"`
	GroupLabels struct {
		Alertname string `json:"alertname"`
	} `json:"groupLabels"`
	Receiver string `json:"receiver"`
	Status   string `json:"status"`
	Version  string `json:"version"`
}

type discordOut struct {
	Content string         `json:"content"`
	Embeds  []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Color       int                 `json:"color"`
	Fields      []discordEmbedField `json:"fields"`
}

type discordEmbedField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

const defaultListenAddress = "127.0.0.1:9094"

var (
	whURL         = flag.String("webhook.url", os.Getenv("DISCORD_WEBHOOK"), "Discord WebHook URL.")
	listenAddress = flag.String("listen.address", os.Getenv("LISTEN_ADDRESS"), "Address:Port to listen on.")
	debug         = flag.Bool("debug", os.Getenv("DEBUG") == "1", "Enable debug mode.")
)

func checkWhURL(whURL string) {
	if whURL == "" {
		log.Fatalf("Environment variable 'DISCORD_WEBHOOK' or CLI parameter 'webhook.url' not found.")
	}
	_, err := url.Parse(whURL)
	if err != nil {
		log.Fatalf("The Discord WebHook URL doesn't seem to be a valid URL.")
	}

	re := regexp.MustCompile(`https://discord(?:app)?.com/api/webhooks/[0-9]{18,19}/[a-zA-Z0-9_-]+`)
	if ok := re.Match([]byte(whURL)); !ok {
		log.Printf("The Discord WebHook URL doesn't seem to be valid.")
	}
}

func sendWebhook(amo *alertManOut) {
	DO := discordOut{
		Content: fmt.Sprintf("=== Alert: %s - %s ===", amo.Receiver, amo.GroupLabels.Alertname),
		Embeds:  []discordEmbed{},
	}
	for _, alert := range amo.Alerts {
		status := alert.Status
		embed := discordEmbed{
			Color:  ColorGrey,
			Fields: []discordEmbedField{},
		}
		switch alert.Status {
		case "firing":
			if s, ok := alert.Labels["severity"]; ok {
				status = s
			}
			embed.Color = getSeverityColor(status)
			break
		case "resolved":
			embed.Color = ColorGreen
			status = "normal"
			break
		}
		startAt, _ := time.ParseInLocation(time.RFC3339, alert.StartsAt, time.Local)
		endAt, _ := time.ParseInLocation(time.RFC3339, alert.EndsAt, time.Local)
		embed.Title = fmt.Sprintf("[%s] %s", strings.ToUpper(status), alert.Annotations.Summary)
		var labels []string
		for k, v := range alert.Labels {
			labels = append(labels, fmt.Sprintf(": - **_%s:_** %s", k, v))
		}
		description := strings.Split(alert.Annotations.Description, "\n")
		for i, v := range description {
			description[i] = fmt.Sprintf(": - %s", v)
		}
		var embedDescribe []string
		eventTime := startAt
		if endAt.After(startAt) {
			eventTime = endAt
		}
		embedDescribe = append(embedDescribe, fmt.Sprintf("**⏰ Event Time:** %s", eventTime.Format(time.DateTime)))
		embedDescribe = append(embedDescribe, fmt.Sprintf("**🏷️ Alert labels:**\n%s", strings.Join(labels, "\n")))
		if status != "normal" {
			// Abnormal state
			embedDescribe = append(embedDescribe, "------")
			embedDescribe = append(embedDescribe, fmt.Sprintf("**📖 Description:**\n%s", strings.Join(description, "\n")))
		} else if endAt.After(startAt) {
			embedDescribe = append(embedDescribe, "------")
			embedDescribe = append(embedDescribe, fmt.Sprintf("**⏲️ Duration:** %s", endAt.Sub(startAt).String()))
			embedDescribe = append(embedDescribe, fmt.Sprintf(": - **_Start:_** %s", startAt.Format(time.DateTime)))
			embedDescribe = append(embedDescribe, fmt.Sprintf(": - **_End:_** %s", endAt.Format(time.DateTime)))
		}
		embed.Description = strings.Join(embedDescribe, "\n")
		DO.Embeds = append(DO.Embeds, embed)
	}
	//
	DOD, _ := json.Marshal(DO)
	if *debug {
		fmt.Println("Send webhook:", string(DOD))
	}
	http.Post(*whURL, "application/json", bytes.NewReader(DOD))
}

func getSeverityColor(severity string) int {
	switch severity {
	case "debug":
		return ColorGrey
	case "info":
		return ColorBlue
	case "warning":
		return ColorOrange
	case "critical":
		return ColorRed
	default:
		return ColorRed
	}
}

func sendRawPromAlertWarn() {
	badString := `This program is suppose to be fed by alertmanager.` + "\n" +
		`It is not a replacement for alertmanager, it is a ` + "\n" +
		`webhook target for it. Please read the README.md  ` + "\n" +
		`for guidance on how to configure it for alertmanager` + "\n" +
		`or https://prometheus.io/docs/alerting/latest/configuration/#webhook_config`

	log.Print(`/!\ -- You have misconfigured this software -- /!\`)
	log.Print(`--- --                                      -- ---`)
	log.Print(badString)

	DO := discordOut{
		Content: "",
		Embeds: []discordEmbed{
			{
				Title:       "You have misconfigured this software",
				Description: badString,
				Color:       ColorGrey,
				Fields:      []discordEmbedField{},
			},
		},
	}

	DOD, _ := json.Marshal(DO)
	http.Post(*whURL, "application/json", bytes.NewReader(DOD))
}

func main() {
	flag.Parse()
	checkWhURL(*whURL)

	if *listenAddress == "" {
		*listenAddress = defaultListenAddress
	}

	log.Printf("Listening on: %s", *listenAddress)
	log.Fatalf("Failed to listen on HTTP: %v",
		http.ListenAndServe(*listenAddress, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("%s - [%s] %s", r.Host, r.Method, r.URL.RawPath)

			b, err := ioutil.ReadAll(r.Body)
			if err != nil {
				panic(err)
			}

			if *debug {
				fmt.Println("Receive webhook:", string(b))
			}

			amo := alertManOut{}
			err = json.Unmarshal(b, &amo)
			if err != nil {
				if isRawPromAlert(b) {
					sendRawPromAlertWarn()
					return
				}

				if len(b) > 1024 {
					log.Printf("Failed to unpack inbound alert request - %s...", string(b[:1023]))

				} else {
					log.Printf("Failed to unpack inbound alert request - %s", string(b))
				}

				return
			}

			sendWebhook(&amo)
		})))
}
