package main

import (
	"bufio"
	"bytes"
	_ "crypto/sha512"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/cheggaaa/pb"
	"github.com/icedream/mixcloud_uploader/confirm"
	"github.com/icedream/mixcloud_uploader/mixcloud"
	"github.com/icedream/mixcloud_uploader/term"
	flag "github.com/juju/gnuflag"
	"github.com/mattn/go-colorable"
)

var VERSION string
var MINVERSION string
var OAUTH_CLIENT_ID string
var OAUTH_CLIENT_SECRET string

var OAUTH_REDIRECT_URI = "https://test.icedream.tech"
var API_URL = "https://api.mixcloud.com/upload/?access_token="
var ACCESS_TOKEN_URL = "https://www.mixcloud.com/oauth/access_token?client_id=" + OAUTH_CLIENT_ID + "&redirect_uri=" + OAUTH_REDIRECT_URI + "&client_secret=" + OAUTH_CLIENT_SECRET + "&code=%s"
var API_ME_URL = "https://api.mixcloud.com/me?access_token="
var CONFIG_FILE = "config.json"
var CONFIG_FILE_PATH = ""

var TRACKLIST_OUTPUT_FORMAT = "%d. %s-%s\n"

var CURRENT_USER mixcloud.User = mixcloud.User{}
var configuration = Configuration{}

var aboutFlag = flag.Bool("about", false, "About the application")
var configFlag = flag.Bool("config", false, "Configure the application")
var fileFlag = flag.String("file", "", "The mp3 file to upload to mixcloud")
var coverFlag = flag.String("cover", "", "The image file to upload to mixcloud as the cover")
var trackListFlag = flag.String("tracklist", "", "A file containing a tracklist for the cloudcast")
var titleFlag = flag.String("title", "", "A title for the cloudcast")
var descriptionFlag = flag.String("description", "", "A description for the cloudcast")
var tagsFlag = flag.String("tags", "", "A comma-separated list of tags to apply to the cloudcast")
var configDirFlag = flag.String("config-dir", "", "A custom directory to store the mixcloud uploader configuration in")

var STD_OUT = bufio.NewWriter(colorable.NewColorableStdout())
var STD_ERR = bufio.NewWriter(colorable.NewColorableStderr())
var STD_IN = bufio.NewReader(os.Stdin)

type Configuration struct {
	ACCESS_TOKEN string
	DEFAULT_TAGS string
}

func showWelcomeMessage() {
	OutputMessage(term.Green + "Mixcloud CLI Uploader v" + VERSION + term.Reset + "\n\n")
}

func showAboutMessage() {
	OutputMessage(fmt.Sprintf("Build Number: %s\n", MINVERSION))
	OutputMessage("Created by: Greg Tangey (http://ignite.digitalignition.net/)\n")
	OutputMessage("Website: http://www.rhythmandpoetry.net/\n")
}

func createConfig() {
	OutputMessage("Creating Configuration File...\n")
	OutputMessage("Please visit the URL below\n\nhttps://www.mixcloud.com/oauth/authorize?client_id=z3CWHgULyawutvpcD3&redirect_uri=http://www.rhythmandpoetry.net/mixcloud_code.php\n")

	OutputMessage("Enter the provided code: ")
	code, err := STD_IN.ReadString('\n')
	if err != nil {
		OutputError("Code Error.")
		os.Exit(2)
	}

	code = strings.TrimSpace(code)
	access_token := fetchAccessCode(code)
	if access_token == "" {
		OutputError("Error fetching access token")
		os.Exit(2)
	} else {
		configuration.ACCESS_TOKEN = access_token
	}

	OutputMessage("Enter default tags (comma separated): ")
	tags, err := STD_IN.ReadString('\n')
	if err != nil {
		OutputError("Incorrect tag format.")
		os.Exit(2)
	} else {
		configuration.DEFAULT_TAGS = strings.TrimSpace(tags)
	}

	saveConfig()
}

func build_http(url string, request string) *http.Request {
	req, err := http.NewRequest(request, url, nil)
	if err != nil {
		OutputError(err.Error())
	}

	req.Header.Set("User-Agent", "Mixcloud CLI Uploader v"+VERSION)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	return req
}

func fetchMe(access_token string) mixcloud.User {

	OutputMessage(term.Green + "Fetching your user data.." + term.Reset + "\n")

	url := API_ME_URL + access_token

	request := build_http(url, "GET")

	client := http.Client{}
	resp, doError := client.Do(request)
	if doError != nil {
		OutputError("Error fetching your profile data: " + doError.Error())
		os.Exit(2)
	}

	var user mixcloud.User

	jsonError := json.NewDecoder(resp.Body).Decode(&user)
	resp.Body.Close()
	if jsonError != nil {
		OutputError("Error decoding response from API - " + jsonError.Error())
		os.Exit(2)
	}

	return user

}

func fetchAccessCode(code string) string {
	url := fmt.Sprintf(ACCESS_TOKEN_URL, code)

	request := build_http(url, "GET")

	client := &http.Client{}
	resp, doError := client.Do(request)
	if doError != nil {
		OutputError("Error fetching Access Code: " + doError.Error())
		os.Exit(2)
	}

	var jsonResponse map[string]interface{}
	jsonError := json.NewDecoder(resp.Body).Decode(&jsonResponse)
	resp.Body.Close()
	if jsonError != nil {
		OutputError("Error decoding response from API - " + jsonError.Error())
		os.Exit(2)
	}

	var access_token = ""

	if jsonResponse["access_token"] != nil {
		access_token = jsonResponse["access_token"].(string)
	}

	return access_token
}

func saveConfig() {
	file, error := os.Create(CONFIG_FILE)
	defer file.Close()
	if error != nil {
		OutputError(fmt.Sprintf("Unable to save configuration file conf.json - ", error))
		os.Exit(2)
	}

	encoder := json.NewEncoder(file)

	err := encoder.Encode(&configuration)
	if err != nil {
		OutputError(fmt.Sprintf("Error writing to config file: %s", err))
		os.Exit(2)
	} else {
		OutputMessage(term.Green + "Configuration saved." + term.Reset + "\n")
	}
}

func loadConfig() {
	file, error := os.Open(CONFIG_FILE)
	defer file.Close()

	if error != nil {
		//Config file doesn't exist, create
		createConfig()
	} else {
		decoder := json.NewDecoder(file)

		err := decoder.Decode(&configuration)
		if err != nil {
			fmt.Println("Error reading config file: ", err)
			os.Exit(2)
		}
	}

	if configuration.ACCESS_TOKEN == "" {
		OutputError("Access Token configuration missing.")
		os.Exit(2)
	}
}

func setupApp() {
	usr, _ := user.Current()
	switch {
	case configDirFlag == nil:
		CONFIG_FILE_PATH = filepath.Join(usr.HomeDir, ".mixcloud")
	default:
		CONFIG_FILE_PATH = *configDirFlag
	}
	CONFIG_FILE = filepath.Join(CONFIG_FILE_PATH, CONFIG_FILE)

	if _, err := os.Stat(CONFIG_FILE_PATH); os.IsNotExist(err) {
		os.Mkdir(CONFIG_FILE_PATH, 0700)
	}
}

func main() {
	flag.Parse(true)

	showWelcomeMessage()
	if *aboutFlag == true {
		showAboutMessage()
		os.Exit(0)
	}

	setupApp()
	loadConfig()

	CURRENT_USER = fetchMe(configuration.ACCESS_TOKEN)

	var tracklist []mixcloud.Track

	if *configFlag == true {
		createConfig()
	}

	if *trackListFlag != "" {
		tracklist = parseTracklist(trackListFlag)
	}

	if *fileFlag == "" {
		OutputError("You must pass a file to upload, use --file or see --help.\n Exiting.")
		os.Exit(2)
	}

	b := &bytes.Buffer{}

	writer := multipart.NewWriter(b)

	var cast_name, cast_desc string
	var tags_arr []string
	if titleFlag != nil || descriptionFlag != nil || tagsFlag != nil {
		// We got flags for name/description/tags, do not ask interactively
		if titleFlag != nil {
			cast_name = *titleFlag
		}
		if descriptionFlag != nil {
			cast_desc = *descriptionFlag
		}
		if tagsFlag != nil {
			tags_arr = strings.Split(*tagsFlag, ",")
		}
	} else {
		cast_name, cast_desc, tags_arr = GetBasicInput()
	}

	BuildBasicHTTPWriter(writer, cast_name, cast_desc, tags_arr, tracklist)
	AddPremiumToHTTPWriter(writer)

	// Add MP3
	if *fileFlag != "" {
		loadFileToWriter(*fileFlag, "mp3", writer)
	}

	// Add cover image
	if *coverFlag != "" {
		loadFileToWriter(*coverFlag, "picture", writer)
	}

	writer.Close()

	// bufReader := bufio.NewReader(b)
	// for line, _, err := bufReader.ReadLine(); err != io.EOF; line, _, err = bufReader.ReadLine() {
	// 	OutputMessage(string(line) + "\n")
	// }

	request, bar := HttpUploadRequest(b, writer)

	bar.Empty = term.Red + "-" + term.Reset
	bar.Current = term.Green + "=" + term.Reset
	client := &http.Client{}
	OutputMessage("\n\n")
	STD_OUT.Flush()
	bar.Start()
	resp, err := client.Do(request)
	if err != nil {
		OutputError("Error: " + err.Error())
		os.Exit(2)
	}
	bar.Finish()

	var Response *mixcloud.Response = new(mixcloud.Response)

	error := json.NewDecoder(resp.Body).Decode(&Response)

	resp.Body.Close()
	if error != nil {
		OutputError("Error decoding response from API - " + error.Error())
		os.Exit(2)
	}

	if handleJSONResponse(*Response) {
		printTracklist(tracklist)
	} else {
		os.Exit(2)
	}
}

func GetBasicInput() (string, string, []string) {
	OutputMessage("Enter a name for the cloudcast: ")
	cast_name, err := STD_IN.ReadString('\n')
	if err != nil {
		OutputError("Incorrect name.")
		os.Exit(2)
	}

	OutputMessage("Enter a description: ")
	cast_desc, err := STD_IN.ReadString('\n')
	if err != nil {
		OutputError("Incorrect description.")
		os.Exit(2)
	}

	OutputMessage(fmt.Sprintf("Enter tags (comma separated) [%s]: ", configuration.DEFAULT_TAGS))
	cast_tags, err := STD_IN.ReadString('\n')
	if err != nil {
		OutputError("Incorrect tag format.")
		os.Exit(2)
	}

	if cast_tags == "" || cast_tags == "\n" {
		cast_tags = configuration.DEFAULT_TAGS
	}
	tags_arr := strings.Split(cast_tags, ",")

	return cast_name, cast_desc, tags_arr
}

func printTracklist(tracklist []mixcloud.Track) {
	OutputMessage("Tracklist\n")
	for i, track := range tracklist {
		OutputMessage(fmt.Sprintf(TRACKLIST_OUTPUT_FORMAT, i+1, track.Artist, track.Song))
	}
}

func parseTracklist(tracklist *string) []mixcloud.Track {
	var list []mixcloud.Track

	fin, err := os.Open(*tracklist)
	if err != nil {
		fmt.Fprintf(os.Stderr, "The file %s does not exist!\n", tracklist)
		return nil
	}
	defer fin.Close()

	/*
		Example contents:
		{
		  "tracklist": [
		    {
		      "title": "Intro",
		      "artist": "",
		      "label": "",
		      "url": "",
		      "time_str": "0:00:00",
		      "time": "0"
		    },
		    {
		      "title": "Sunstroke (Facade's J to the Y Remix)",
		      "artist": "Chicane",
		      "label": "",
		      "url": "https://soundcloud.com/adamthomasmusic/free-download-chicane-sunstroke-facades-j-to-the-y-remix",
		      "time_str": "0:01:04",
		      "time": "64"
		    },
		    ...
		    {
		      "title": "Isolator",
		      "artist": "Will Atkinson",
		      "label": "Subculture",
		      "url": "",
		      "time_str": "2:01:31",
		      "time": "7291"
		    }
		  ],
		  "episode": "007"
		}
	*/

	tracklistObj := new(struct {
		Tracklist []struct {
			Title      string
			Artist     string
			Label      string
			Url        string
			TimeString string `json:"time_str"`
			Time       int
		}
	})
	if err = json.NewDecoder(fin).Decode(tracklistObj); err != nil {
		fmt.Fprintf(os.Stderr, "The file %s can not be parsed: %s!\n", tracklist, err.Error())
		return nil
	}

	for _, entry := range tracklistObj.Tracklist {
		thistrack := new(mixcloud.Track)

		thistrack.Artist = entry.Artist
		thistrack.Song = entry.Title
		thistrack.Time = entry.Time

		list = append(list, *thistrack)

		// if !isPrefix {
		//   fmt.Printf("Lines: %s (error %v)\n", string(bytes), err)
		//   bytes = bytes[:0]
		// }

	}

	return list
}

func handleJSONResponse(response mixcloud.Response) bool {
	if response.Error != nil {
		OutputError(response.Error.Message)
		fmt.Printf("%v", response.Details)
		return false
	} else if response.Result.Success {
		OutputMessage(term.Green + "Sucessfully uploaded file" + term.Reset + "\n")
		path := response.Result.Key
		OutputMessage(term.Green + "https://mixcloud.com" + path + "edit" + term.Reset + "\n")
		return true
	} else {
		OutputError("Error uploading, no success")
		fmt.Printf("%v", response)
		return false
	}
}

func OutputError(message string) {
	STD_ERR.WriteString(term.Bold + term.Red + message + term.Reset + "\n")
	STD_ERR.Flush()
}

func OutputMessage(message string) {
	STD_OUT.WriteString(message)
	STD_OUT.Flush()
}

func loadFileToWriter(file string, key string, writer *multipart.Writer) {
	f, err := os.Open(file)
	if err != nil {
		OutputError("Error opening file " + file + "\n")
		os.Exit(2)
	}
	defer f.Close()

	fw, err := writer.CreateFormFile(key, file)
	if err != nil {
		OutputError("Error reading file " + file + "\n")
		os.Exit(2)
	}

	if _, err = io.Copy(fw, f); err != nil {
		OutputError("Error opening file " + file + " to buffer\n")
		os.Exit(2)
	}
}

func BuildBasicHTTPWriter(writer *multipart.Writer, name string, desc string, tag_list []string, tracklist []mixcloud.Track) {
	// Add information name/description
	writer.WriteField("name", name)
	writer.WriteField("description", desc)

	// Add tags
	for i, tag := range tag_list {
		field_name := fmt.Sprintf("tags-%d-tag", i)
		writer.WriteField(field_name, tag)
	}

	// Add tracklist
	if tracklist != nil {
		for i, track := range tracklist {
			artist_field_name := fmt.Sprintf("sections-%d-artist", i)
			song_field_name := fmt.Sprintf("sections-%d-song", i)
			duration_field_name := fmt.Sprintf("sections-%d-start_time", i)

			writer.WriteField(artist_field_name, track.Artist)
			writer.WriteField(song_field_name, track.Song)
			writer.WriteField(duration_field_name, fmt.Sprintf("%d", track.Time))
		}
	}
}

func ParseDateInputToTime(dateIn string) time.Time {
	location, err := time.LoadLocation("Local")

	dateTime, err := time.ParseInLocation("02/01/2006 15:04", strings.TrimSpace(dateIn), location)

	if err != nil {
		OutputError("Incorrect date format  - " + err.Error())
		os.Exit(2)
	}

	return dateTime
}

func AddPremiumToHTTPWriter(writer *multipart.Writer) {

	// If you're not PRO, you can't do this, get out
	if !CURRENT_USER.IsPro {
		return
	}

	OutputMessage("\n" + term.Green + "Setting pro user attributes..." + term.Reset + "\n")

	publish_date, disable_comments, hide_stats, unlisted := GetPremiumInput()

	if publish_date != "" {
		writer.WriteField("publish_date", publish_date)
	}
	if disable_comments {
		writer.WriteField("disable_comments", "1")
	}
	if hide_stats {
		writer.WriteField("hide_stats", "1")
	}
	if unlisted {
		writer.WriteField("unlisted", "1")
	}
}

func GetPremiumInput() (string, bool, bool, bool) {
	disable_comments := false
	hide_stats := false
	unlisted := false
	publish_date := ""

	fmt.Printf("Disable comments? [y/n] ")
	if confirm.AskForConfirmation() {
		disable_comments = true
	}

	fmt.Printf("Hide statistics? [y/n] ")
	if confirm.AskForConfirmation() {
		hide_stats = true
	}

	fmt.Printf("Set to unlisted? [y/n] ")
	if confirm.AskForConfirmation() {
		unlisted = true
	}

	fmt.Printf("Set publish date? [y/n] ")
	if confirm.AskForConfirmation() {
		publish_date = PublishDateInput()

	}

	return publish_date, disable_comments, hide_stats, unlisted
}

func PublishDateInput() string {
	current_time := time.Now().In(time.Local)
	zonename, offset := current_time.Zone()

	OutputMessage("Enter a publish date in " + zonename + " (" + fmt.Sprintf("%+d", offset/60/60) + " GMT) [DD/MM/YYYY HH:MM]: ")
	inPublishDate, err := STD_IN.ReadString('\n')
	if err != nil {
		OutputError("Incorrect publish date.")
		os.Exit(2)
	}

	publish_date := ParseDateInputToTime(inPublishDate)

	if !publish_date.After(current_time) {
		OutputError("Date " + publish_date.Format(time.RFC1123) + " is not in the future")
		return PublishDateInput()
	}

	return publish_date.UTC().Format(time.RFC3339)
}

func HttpUploadRequest(b *bytes.Buffer, writer *multipart.Writer) (*http.Request, *pb.ProgressBar) {

	url := API_URL + configuration.ACCESS_TOKEN

	var bar = pb.New(b.Len()).SetUnits(pb.U_BYTES)
	reader := bar.NewProxyReader(b)

	request, err := http.NewRequest("POST", url, reader)
	if err != nil {
		OutputError("Error building request")
		os.Exit(2)
	}

	request.Header.Add("Content-Type", writer.FormDataContentType())

	return request, bar
}
