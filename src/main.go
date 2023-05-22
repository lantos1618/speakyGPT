package main

import (
	"context"
	"net/http"

	"crypto/sha256"
	"encoding/hex"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	"github.com/gin-gonic/gin"
)

func getHash(voiceName string, text string) string {
	h := sha256.New()
	h.Write([]byte(voiceName + text))
	return hex.EncodeToString(h.Sum(nil))
}

// ListVoices lists the available voices.
func handleListVoices(ttsClient *texttospeech.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		// Get the list of voices
		resp, err := ttsClient.ListVoices(ctx, &texttospeechpb.ListVoicesRequest{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Create a slice to hold the voice data
		voices := make([]map[string]interface{}, len(resp.Voices))

		// Iterate over the voices and add their data to the slice
		for i, voice := range resp.Voices {
			voices[i] = map[string]interface{}{
				"name":                   voice.Name,
				"languageCode":           voice.LanguageCodes,
				"ssmlGender":             voice.SsmlGender.String(),
				"naturalSampleRateHertz": voice.NaturalSampleRateHertz,
			}
		}

		// Return the list of voices
		c.JSON(http.StatusOK, gin.H{
			"voices": voices,
		})
	}
}

func handleListLanguages(ttsClient *texttospeech.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		// Get the list of voices
		resp, err := ttsClient.ListVoices(ctx, &texttospeechpb.ListVoicesRequest{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Create a set to hold the languages (to remove duplicates)
		languages := make(map[string]bool)

		// Iterate over the voices and add their languages to the set
		for _, voice := range resp.Voices {
			for _, language := range voice.LanguageCodes {
				languages[language] = true
			}
		}

		// Convert the set to a slice
		languageSlice := make([]string, 0, len(languages))
		for language := range languages {
			languageSlice = append(languageSlice, language)
		}

		// Return the list of languages
		c.JSON(http.StatusOK, gin.H{
			"languages": languageSlice,
		})
	}
}

type TTSRequest struct {
	VoiceName string `json:"voice_name"`
	Text      string `json:"text"`
}

// TTS generates audio from text.
func handleTTS(ttsClient *texttospeech.Client, fsClient *firestore.Client, bucket *storage.BucketHandle, bucketName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var ttsReq TTSRequest

		if err := c.ShouldBindJSON(&ttsReq); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		voiceName := ttsReq.VoiceName
		text := ttsReq.Text
		fileName := getHash(voiceName, text) + ".mp3"

		// Synthesize speech
		ctx := context.Background()
		req := &texttospeechpb.SynthesizeSpeechRequest{
			Input: &texttospeechpb.SynthesisInput{
				InputSource: &texttospeechpb.SynthesisInput_Text{Text: text},
			},
			Voice: &texttospeechpb.VoiceSelectionParams{
				LanguageCode: "en-US",
				SsmlGender:   texttospeechpb.SsmlVoiceGender_FEMALE,
			},
			AudioConfig: &texttospeechpb.AudioConfig{
				AudioEncoding: texttospeechpb.AudioEncoding_MP3,
			},
		}

		resp, err := ttsClient.SynthesizeSpeech(ctx, req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Upload audio to Storage
		obj := bucket.Object(voiceName + ".mp3")
		w := obj.NewWriter(ctx)
		if _, err := w.Write(resp.AudioContent); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := w.Close(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Add document to Firestore
		_, _, err = fsClient.Collection("queries").Add(ctx, map[string]interface{}{
			"voice_name": voiceName,
			"text":       text,
			"audio_url":  "https://storage.googleapis.com/" + bucketName + "/" + fileName + ".mp3",
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"audio_url": "https://storage.googleapis.com/" + bucketName + "/" + fileName + ".mp3",
		})
	}
}

func main() {
	r := gin.Default()

	// Serve files from the "public" directory
	r.StaticFS("/public", http.Dir("public"))

	ctx := context.Background()

	// Initialize Text-to-Speech client
	ttsClient, err := texttospeech.NewClient(ctx)
	if err != nil {
		panic(err)
	}
	defer ttsClient.Close()

	// Initialize Firestore client, used to store audio files
	fsClient, err := firestore.NewClient(ctx, "your-project-id")
	if err != nil {
		panic(err)
	}
	defer fsClient.Close()

	// Initialize Storage client
	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		panic(err)
	}
	defer storageClient.Close()

	bucketName := "your-bucket-name"
	bucket := storageClient.Bucket(bucketName)

	r.GET("/listVoices/:language_code", handleListVoices(ttsClient))

	r.GET("/listLanguages", handleListLanguages(ttsClient))

	r.POST("/tts", handleTTS(ttsClient, fsClient, bucket, bucketName))

	r.Run() // listen and serve on 0.0.0.0:8080
}
