package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dghubble/oauth1"
	"github.com/g8rswimmer/go-twitter/v2"
)

const mediaURL = "https://upload.twitter.com/1.1/media/upload.json"

var epoch = time.Date(2025, 4, 11, 0, 0, 0, 0, time.UTC)

func main() {
	imageHost := flag.String("ih", "", "image host")
	groupName := flag.String("t", "mikan", "group name")
	cameraName := flag.String("c", "camera-a", "camera name")
	consumerKey := flag.String("ck", "", "X consumer key")
	consumerSecret := flag.String("cs", "", "X consumer secret")
	accessToken := flag.String("at", "", "X access token")
	accessSecret := flag.String("as", "", "X access secret")
	flag.Parse()
	if *imageHost == "" {
		log.Fatal("image host is required")
	}
	if *consumerKey == "" || *consumerSecret == "" || *accessToken == "" || *accessSecret == "" {
		log.Fatal("Consumer key/secret and Access token/secret are required")
	}

	// Download latest image
	url, err := latestImageURL(*imageHost, *groupName, *cameraName)
	if err != nil {
		log.Fatal(err)
	}
	filePath := "latest.jpg"
	if err = download(url, filePath); err != nil {
		log.Fatal(err)
	}
	defer func() {
		if rmErr := os.Remove(filePath); rmErr != nil {
			log.Printf("failed to remove %s: %v", filePath, rmErr)
		}
	}()

	// Upload latest image
	config := oauth1.NewConfig(*consumerKey, *consumerSecret)
	token := oauth1.NewToken(*accessToken, *accessSecret)
	httpClient := config.Client(oauth1.NoContext, token)
	mediaID, err := uploadMedia(filePath, httpClient)
	if err != nil {
		log.Fatal(err)
	}

	// Post
	status := fmt.Sprintf("DAY %d", int(time.Since(epoch).Hours()/24)+1)
	if err = post(context.Background(), config, token, status, mediaID); err != nil {
		log.Fatal(err)
	}
}

func safeClose(closer io.Closer, name string) {
	if err := closer.Close(); err != nil {
		log.Printf("failed to close %s: %v", name, err)
	}
}

func latestImageURL(imageHost, groupName, cameraName string) (string, error) {
	resp, err := http.Get("https://" + imageHost + "/still/" + groupName + "/" + cameraName + "/latest.txt")
	if err != nil {
		return "", fmt.Errorf("failed to get latest.txt: %w", err)
	}
	defer safeClose(resp.Body, "latest.txt response")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read latest.txt response: %w", err)
	}
	if !strings.HasSuffix(string(body), ".jpg") {
		return "", fmt.Errorf("unexpected latest.txt response: %s", string(body))
	}
	return "https://" + imageHost + "/" + string(body), nil
}

func download(url, filePath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer safeClose(resp.Body, "image response")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read image response: %w", err)
	}
	return os.WriteFile(filePath, body, 0644)
}

func uploadMedia(filePath string, httpClient *http.Client) (string, error) {
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	part, err := w.CreateFormFile("media", filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create form: %w", err)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open %s :%w", filePath, err)
	}
	if _, err = io.Copy(part, file); err != nil {
		return "", fmt.Errorf("failed to copy %s: %w", filePath, err)
	}
	if err = w.Close(); err != nil {
		return "", fmt.Errorf("failed to close form: %w", err)
	}
	resp, err := httpClient.Post(mediaURL, w.FormDataContentType(), bytes.NewReader(buf.Bytes()))
	if err != nil {
		return "", fmt.Errorf("failed to upload media: %w", err)
	}
	defer safeClose(resp.Body, "media upload response")
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read media upload response: %w", err)
	}
	log.Printf("media upload response: HTTP %d %s", resp.StatusCode, string(body))
	mediaUploadResponse := struct {
		MediaIDString string `json:"media_id_string"`
	}{}
	if err = json.Unmarshal(body, &mediaUploadResponse); err != nil {
		return "", fmt.Errorf("failed to unmarshal media upload response: %w", err)
	}
	return mediaUploadResponse.MediaIDString, nil
}

type dummyAuthorizer struct{ Token string }

func (a dummyAuthorizer) Add(_ *http.Request) {}

func post(ctx context.Context, config *oauth1.Config, token *oauth1.Token, status, mediaID string) error {
	client := &twitter.Client{
		Authorizer: dummyAuthorizer{},
		Client:     config.Client(oauth1.NoContext, token),
		Host:       "https://api.twitter.com",
	}
	resp, err := client.CreateTweet(ctx, twitter.CreateTweetRequest{
		Text:  status,
		Media: &twitter.CreateTweetMedia{IDs: []string{mediaID}},
	})
	if err != nil {
		return fmt.Errorf("failed to post: %w\n", err)
	}
	log.Printf("posted: %s (rate limit: %d/%d)", resp.Tweet.ID, resp.RateLimit.Remaining, resp.RateLimit.Limit)
	return nil
}
