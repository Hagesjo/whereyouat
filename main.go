package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hagesjo/godiscord"
)

type Env struct {
	Token                    string `json:"token"`
	MainGuildID              string `json:"main_guild_id"`
	SelfID                   string `json:"self_id"`
	PublicApplicationChannel string `json:"public_application_channel"`
	WowAHClientID            string `json:"wowah_client_id"`
	WowAHClientSecret        string `json:"wowah_client_secret"`
	WowAHTokenChannel        string `json:"wowah_token_channel"`
}

func toPtr[T any](t T) *T {
	return &t
}

var currentBotFormat = `### **1.** What's your name?
aoeuaoeuaoeuoeuoeuaoeuaoeuaoeu
### **2.** How old are you?
aoeu
### **3.** Where are you from?
aoeu
### **4.** Tell us a little bit about yourself.
aoeu
### **5.** What's your battle tag?
aoeu
### **6.** What's your ingame name?
aoeuaoeuaoeuoeuoeuaoeuaoeuaoeu
### **7.** What class do you play?
aoeu
### **8.** What role do you play?
DPS
### **9.** Link us some logs.
aoeu
### **10.** Do you play any alts?
aoeu
### **11.** What raiding experience do you have?
aoeu
### **12.** A photo of your combat UI (You can upload an image to discord)
aoeu
### **13.** Why are you leaving your current guild?
aoeu
### **14.** Why should we pick you?
ueoa
### **15.** Our raid times are Monday and Wednesday, 19.00 till 22.00 ST. Invites roll out at 18.45.  Can you make these times?
Yes
`

func main() {
	f, err := os.Open("env.json")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	var env Env
	if err := json.NewDecoder(f).Decode(&env); err != nil {
		panic(err)
	}

	bot, err := godiscord.NewBot(env.Token, "!")
	if err != nil {
		panic(err)
	}

	if err := bot.RegisterEventListener(func(f *godiscord.Fetcher, de godiscord.MessageCreate) error {
		c, ok := f.GetChannelByID(de.ChannelID)
		if !ok {
			// It's probably a thread, ignore.
			return nil
		}

		if c.Name == nil || *c.Name != "applications" {
			return nil
		}

		// Just for safety, don't want loops - probably overkill.
		if de.Author.ID == env.SelfID {
			return nil
		}

		if de.Author.Username != "Appy" {
			return nil
		}

		if len(de.Embeds) != 1 {
			fmt.Printf("got %d embeds\n", len(de.Embeds))
			return nil
		}

		var filteredRows []string
		for i, r := range strings.Split(*de.Message.Embeds[0].Description, "###") {
			if i == 5 { // Discord tag
				continue
			}

			filteredRows = append(filteredRows, r)
		}

		publicChannel, ok := f.GetChannelByName(env.PublicApplicationChannel)
		if !ok {
			return fmt.Errorf("public application channel not found")
		}

		msg, err := f.SendEmbeds(publicChannel.ID, []godiscord.Embed{
			{
				Title:       toPtr("Application received"),
				Description: toPtr(strings.Join(filteredRows, "###")),
			},
		})
		if err != nil {
			return fmt.Errorf("failed to send embeds: %w", err)
		}

		nameSplits := strings.Split(filteredRows[1], "\n")
		threadName := nameSplits[1]
		threadName = threadName[:min(len(threadName), 15)]
		_, err = f.CreateThread(publicChannel.ID, msg.ID, godiscord.CreateThreadRequest{
			Name:                threadName,
			AutoArchiveDuration: toPtr(1440),
		})
		if err != nil {
			panic(err)
		}

		return nil
	}); err != nil {
		panic(fmt.Sprintf("failed to register event listener: %s", err))
	}

	go func() {
		if err := bot.Run(); err != nil {
			panic(err)
		}
	}()

	w, err := newWowAH(env.WowAHClientID, env.WowAHClientSecret)
	if err != nil {
		log.Fatal(err)
	}
	lastToken := WowToken{}
	go func() {
		ticker := time.NewTicker(30 * time.Second)

		for {
			<-ticker.C //
			slog.Info("checking token")
			token, err := w.GetToken()
			if err != nil {
				slog.Info("failed to get token: %s", err.Error())
			}
			if lastToken.LastUpdatedTimestamp != token.LastUpdatedTimestamp && lastToken.Price != token.Price {
				diff := (token.Price - lastToken.Price) / 10000
				diffPrefix := ""
				if diff > 0 {
					diffPrefix = "+"
				}

				var msg string
				if lastToken.LastUpdatedTimestamp == 0 {
					msg = fmt.Sprintf("Wow token price: %dg (bot startup)", token.Price/10000)
				} else {
					msg = fmt.Sprintf("Wow token price updated: %dg (%s%d)", token.Price/10000, diffPrefix, diff)
				}
				if err := bot.SendMessage("Pixelbased Lifeforms", "wow-token", msg); err != nil {
					slog.Info("failed to send message: %s", err.Error())
				}
			} else {
				slog.Info("no diff")
			}
			lastToken = token
		}
	}()

	router := http.NewServeMux()
	router.HandleFunc("GET /", Index(bot, env.MainGuildID))
	router.HandleFunc("GET /api/v1/guilds/", GetUsers(bot, env.MainGuildID))

	// Static.
	fs := http.FileServer(http.Dir("static"))
	router.Handle("GET /static/", http.StripPrefix("/static/", fs))

	slog.Info("Serving 7778")
	http.ListenAndServe(":7778", router)
}

type User struct {
	GlobalName string `json:"global_name"`
	Username   string `json:"username"`
	Nick       string `json:"nick"`
	Avatar     string `json:"avatar"`
}

type Channel struct {
	URL   string `json:"url"`
	Name  string `json:"name"`
	Users []User `json:"users"`
}

type Guild struct {
	Name     string    `json:"name,omitempty"`
	Icon     string    `json:"icon,omitempty"`
	Channels []Channel `json:"channels,omitempty"`

	// To speed up sorting.
	numMembers int
}
type Guilds []Guild

func GetUsers(bot *godiscord.Bot, mainGuildID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		onlyGuildies := r.URL.Query().Get("only_guildies")
		resp, err := getData(bot, mainGuildID, onlyGuildies == "true")
		if err != nil {
			slog.Warn("failed to get data", "error", err)
			w.WriteHeader(http.StatusBadGateway)
			return
		}

		j, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			slog.Warn("failed to get users", "error", err)
			w.WriteHeader(http.StatusBadGateway)
			return
		}

		w.Write(j)
	}
}

// Better frontend can wait, will just use go's templating for now.

func Index(bot *godiscord.Bot, mainGuildID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		onlyGuildies := r.URL.Query().Get("only_guildies")
		resp, err := getData(bot, mainGuildID, onlyGuildies == "true")
		if err != nil {
			slog.Warn("failed to get data", "error", err)
		}

		t, err := template.ParseFiles("index.html")
		if err != nil {
			slog.Warn("failed to parse template", "error", err)
			w.WriteHeader(http.StatusBadGateway)
			return
		}

		if err := t.Execute(w, resp); err != nil {
			return
		}
	}
}

func getData(bot *godiscord.Bot, mainGuildID string, onlyGuildies bool) (Guilds, error) {
	guildMembersID := make(map[string]bool)
	if onlyGuildies {
		mainGuild, err := bot.GetGuildByID(mainGuildID)
		if err != nil {
			return Guilds{}, fmt.Errorf("failed to get guild discord: %w", err)
		}

		members, err := bot.GetMembers(mainGuild.ID)
		if err != nil {
			return Guilds{}, fmt.Errorf("failed to get members: %w", err)
		}

		for _, member := range members {
			guildMembersID[member.User.ID] = true
		}
	}

	var guilds Guilds

	// TODO: Can listen to the voice events and cache stuff.
	for _, guild := range bot.ListGuilds() {
		states, err := bot.GetVoiceStates(guild.ID)
		if err != nil {
			return Guilds{}, fmt.Errorf("failed to get voice states: %w", err)
		}

		channelsByGuild := make(map[string]*Channel)
		var numMembers int
		for _, state := range states {
			dChans, err := bot.GetChannelsByIDs(guild.ID, *state.ChannelID)
			if err != nil {
				return Guilds{}, fmt.Errorf("failed to get channels: %w", err)
			}

			channel := &Channel{
				URL:  fmt.Sprintf("https://discord.com/channels/%s/%s", guild.ID, dChans[0].ID),
				Name: *dChans[0].Name,
			}

			if foundChannel, ok := channelsByGuild[guild.ID]; ok {
				channel = foundChannel
			}

			nick := ""
			if state.Member.Nick != nil {
				nick = *state.Member.Nick
			}

			addUser := true
			if onlyGuildies {
				_, addUser = guildMembersID[state.Member.User.ID]
			}

			if addUser {
				channel.Users = append(channel.Users, User{
					GlobalName: state.Member.User.GlobalName,
					Username:   state.Member.User.Username,
					Nick:       nick,
					Avatar:     fmt.Sprintf("http://cdn.discordapp.com/avatars/%s/%s.png", state.Member.User.ID, state.Member.User.Avatar),
				})
				numMembers++
			}

			channelsByGuild[guild.ID] = channel
		}

		var channels []Channel
		for _, c := range channelsByGuild {
			sort.Slice(c.Users, func(i, j int) bool {
				return c.Users[i].GlobalName < c.Users[j].GlobalName
			})
			channels = append(channels, *c)
		}

		g := Guild{
			Channels:   channels,
			Name:       guild.Name,
			numMembers: numMembers,
		}

		if guild.Icon != nil {
			g.Icon = fmt.Sprintf("https://cdn.discordapp.com/icons/%s/%s.png", guild.ID, *guild.Icon)
		}

		guilds = append(guilds, g)
	}

	sort.Slice(guilds, func(i, j int) bool {
		if guilds[i].numMembers != guilds[j].numMembers {
			return guilds[i].numMembers > guilds[j].numMembers
		}

		return guilds[i].Name < guilds[j].Name
	})

	return guilds, nil
}
