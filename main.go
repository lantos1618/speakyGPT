package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"crypto/sha256"
	"encoding/hex"

	"github.com/joho/godotenv"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	"github.com/gin-contrib/cors"
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

		// get the language code from the url query string
		languageCode := c.Param("languageCode")
		fmt.Println("languageCode", languageCode)
		if languageCode == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "languageCode is required"})
			return
		}
		// Get the list of voices
		resp, err := ttsClient.ListVoices(ctx, &texttospeechpb.ListVoicesRequest{
			LanguageCode: languageCode,
		})
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
	VoiceName      string `json:"voiceName" binding:"required"`
	TextNative     string `json:"textNative" binding:"required"`
	TextTranslated string `json:"textTranslated" binding:"required"`
}

func getLanguageCode(voiceName string) (string, error) {
	// Parse the voiceName to extract the language code, the voice type, and the voice number
	parts := strings.SplitN(voiceName, "-", 3)
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid voice name: %s", voiceName)
	}
	languageCode := parts[0] + "-" + parts[1]

	return languageCode, nil
}

func synthesizeSpeech(ttsClient *texttospeech.Client, ttsRequest *TTSRequest) ([]byte, error) {
	ctx := context.Background()

	languageCode, err := getLanguageCode(ttsRequest.VoiceName)
	if err != nil {
		return nil, err
	}

	req := &texttospeechpb.SynthesizeSpeechRequest{
		Input: &texttospeechpb.SynthesisInput{
			InputSource: &texttospeechpb.SynthesisInput_Text{Text: ttsRequest.TextTranslated},
		},
		Voice: &texttospeechpb.VoiceSelectionParams{
			LanguageCode: languageCode,
			Name:         ttsRequest.VoiceName,
		},
		AudioConfig: &texttospeechpb.AudioConfig{
			AudioEncoding: texttospeechpb.AudioEncoding_MP3,
		},
	}

	resp, err := ttsClient.SynthesizeSpeech(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp.AudioContent, nil
}

type TTSObject struct {
	VoiceName      string `json:"voiceName"`
	TextNative     string `json:"textNative"`
	TextTranslated string `json:"textTranslated"`
	FileName       string `json:"fileName"`
	AudioRef       string `json:"audioRef"`
	URL            string `json:"url"`
}

func fireBaseUploadAudio(fsClient *firestore.Client, bucket *storage.BucketHandle, voiceName, textNative, textTranslated, filename string, audioContent []byte) (*firestore.DocumentRef, *TTSObject, error) {
	ctx := context.Background()

	// Upload the audio file to Firebase Storage
	obj := bucket.Object(filename)
	w := obj.NewWriter(ctx)
	if _, err := w.Write(audioContent); err != nil {
		return nil, nil, err
	}

	if err := w.Close(); err != nil {
		return nil, nil, err
	}

	// Make the object publicly readable
	if err := obj.ACL().Set(ctx, storage.AllUsers, storage.RoleReader); err != nil {
		return nil, nil, err
	}

	// change the content type
	// obj.Update(ctx, storage.ObjectAttrsToUpdate{
	// 	ContentType: "audio/mpeg",
	// })

	// Get the URL of the uploaded audio file
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		return nil, nil, err
	}
	storageURL := attrs.MediaLink

	bucketAttrs, err := bucket.Attrs(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Store a document reference in Firestore
	data := TTSObject{
		VoiceName:      voiceName,
		TextNative:     textNative,
		TextTranslated: textTranslated,
		FileName:       filename,
		AudioRef:       "gs://" + bucketAttrs.Name + "/" + filename,
		URL:            storageURL,
	}

	// fmt.Println(data)

	docRef := fsClient.Collection("audio").Doc(filename)
	_, err = docRef.Set(ctx, data)
	if err != nil {
		return nil, nil, err
	}

	return docRef, &data, nil
}

// TTS generates audio from text.
func handleTTS(ttsClient *texttospeech.Client, fsClient *firestore.Client, bucket *storage.BucketHandle, bucketName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req TTSRequest

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		fmt.Println("handleTTS")
		fmt.Println(req)

		voiceName := req.VoiceName
		textNative := req.TextNative
		textTranslated := req.TextTranslated
		filename := getHash(voiceName, textNative) + ".mp3"

		// Synthesize speech
		audioContent, err := synthesizeSpeech(ttsClient, &TTSRequest{
			VoiceName:      voiceName,
			TextNative:     textNative,
			TextTranslated: textTranslated,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		docRef, data, err := fireBaseUploadAudio(fsClient, bucket, voiceName, textNative, textTranslated, filename, audioContent)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"audioUrl": "http://localhost:8080/audio/" + data.FileName,
			"docRef":   docRef,
		})
	}
}

func handleDisplayAudio(fsClient *firestore.Client, bucket *storage.BucketHandle) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := context.Background()

		// Get the document ID from the URL
		docID := c.Param("id")

		// Fetch the document from Firestore
		docRef := fsClient.Collection("audio").Doc(docID)
		docSnapshot, err := docRef.Get(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get document from Firestore"})
			return
		}

		// Decode the document data into a TTSObject
		var data TTSObject
		if err := docSnapshot.DataTo(&data); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode document data"})
			return
		}

		// render some HTML to show the text and audio file
		c.HTML(http.StatusOK, "audio.tmpl", gin.H{
			"audioUrl":       data.URL,
			"voiceName":      data.VoiceName,
			"textNative":     data.TextNative,
			"textTranslated": data.TextTranslated,
		})

	}
}

func main() {
	// load environment variables
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	r := gin.Default()

	// Enable CORS for all origins
	config := cors.DefaultConfig()
	config.AllowOrigins = []string{"https://chat.openai.com", "http://localhost:3000", "http://localhost:8080"}
	config.AllowCredentials = true
	config.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
	config.AllowHeaders = []string{"*"}

	config.ExposeHeaders = []string{"Content-Length", "Content-Type"}

	r.Use(cors.New(config))

	ctx := context.Background()

	// Initialize Text-to-Speech client
	ttsClient, err := texttospeech.NewClient(ctx)
	if err != nil {
		panic(err)
	}
	defer ttsClient.Close()

	// Initialize Firestore client, used to store audio files
	projectName := os.Getenv("GOOGLE_PROJECT_ID")
	fsClient, err := firestore.NewClient(ctx, projectName)
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

	bucketName := os.Getenv("FIREBASE_STORAGE_BUCKET")
	bucket := storageClient.Bucket(bucketName)

	// Load the HTML template
	r.LoadHTMLGlob("templates/*")

	r.GET("/listVoices/:languageCode", handleListVoices(ttsClient))

	r.GET("/listLanguages", handleListLanguages(ttsClient))

	r.POST("/tts", handleTTS(ttsClient, fsClient, bucket, bucketName))

	r.GET("/audio/:id", handleDisplayAudio(fsClient, bucket))

	// Serve files from the "public" directory
	r.StaticFS("/public", http.Dir("public"))
	r.StaticFS("/.well-known", http.Dir(".well-known"))

	r.Run() // listen and serve on 0.0.0.0:8080
}
