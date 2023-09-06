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
	"strconv"
	"strings"
	"time"
)

// Discord color values
const (
	MaxDiscordEmbed = 10
	ColorRed        = 0x992D22
	ColorOrange     = 0xF0B816
	ColorGreen      = 0x2ECC71
	ColorGrey       = 0x95A5A6
	ColorBlue       = 0x58b9ff
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
	var embeds []discordEmbed
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
		loc, _ := time.LoadLocation(os.Getenv("TZ"))
		startAt, _ := time.Parse(time.RFC3339, alert.StartsAt)
		startAt = startAt.In(loc)
		endAt, _ := time.Parse(time.RFC3339, alert.EndsAt)
		endAt = endAt.In(loc)
		embed.Title = fmt.Sprintf("[%s] %s", strings.ToUpper(status), alert.Annotations.Summary)
		var labels []string
		var metricsValue float64
		var metricsConv string
		for k, v := range alert.Labels {
			if strings.HasPrefix(k, "metrics_") {
				// ignore metrics
				if k == "metrics_value" {
					metricsValue, _ = strconv.ParseFloat(v, 64)
				} else if k == "metrics_conv" {
					metricsConv = v
				}
				continue
			}
			// Ignore some key
			if k == "value" {
				continue
			}
			labels = append(labels, fmt.Sprintf(": - **_%s:_** %s", k, v))
		}
		if status != "normal" {
			labels = append(labels, fmt.Sprintf(": - **_value:_** %s", valueConv(metricsValue, metricsConv)))
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
		// Abnormal state
		embedDescribe = append(embedDescribe, "------")
		embedDescribe = append(embedDescribe, fmt.Sprintf("**📖 Description:**\n%s", strings.Join(description, "\n")))
		if endAt.After(startAt) {
			embedDescribe = append(embedDescribe, "------")
			embedDescribe = append(embedDescribe, fmt.Sprintf("**⏲️ Duration:** %s", endAt.Sub(startAt).String()))
			embedDescribe = append(embedDescribe, fmt.Sprintf(": - **_Start:_** %s", startAt.Format(time.DateTime)))
			embedDescribe = append(embedDescribe, fmt.Sprintf(": - **_End:_** %s", endAt.Format(time.DateTime)))
		}
		embed.Description = strings.Join(embedDescribe, "\n")
		embeds = append(embeds, embed)
	}

	if len(embeds) > MaxDiscordEmbed {
		// Set bulk send
		for i := 0; i < len(embeds); i += MaxDiscordEmbed {
			if i+MaxDiscordEmbed <= len(embeds) {
				DO.Embeds = embeds[i : i+MaxDiscordEmbed]
			} else {
				DO.Embeds = embeds[i:]
			}
			fireMessageOut(DO)
		}
	} else {
		DO.Embeds = embeds
		fireMessageOut(DO)
	}
}

func fireMessageOut(msg discordOut) {
	DOD, _ := json.Marshal(msg)
	if *debug {
		fmt.Println("Send webhook:", string(DOD))
	}
	r, err := http.Post(*whURL, "application/json", bytes.NewReader(DOD))
	if err != nil {
		log.Println("Send discord error -:", err)
		return
	}
	if r.StatusCode >= http.StatusBadRequest {
		b, _ := ioutil.ReadAll(r.Body)
		log.Println("Discord server return error -:", r.StatusCode, string(b))
	}
}

func valueConv(v float64, conv string) string {
	switch conv {
	case "duration":
		// input is second
		d := time.Duration(int64(v)) * time.Second
		return d.String()
	case "timestamp":
		t := time.Unix(int64(v), 0)
		return t.Format(time.DateTime)
	case "updown":
		if v == 1.0 {
			return "up"
		}
		return "down"
	}
	return fmt.Sprintf("%.2f", v)
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
			log.Printf("%s - [%s] %s", r.Host, r.Method, r.URL)

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
