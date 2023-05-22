package main

import (
	"context"
	"fmt"

	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	texttospeech "cloud.google.com/go/texttospeech/apiv1"
)

func TestGetHash(t *testing.T) {
	voiceName := "en-US-Wavenet-A"
	text := "Hello, world!"
	expectedHash := "905299e95c365d6bcfe81de24c5d02a9ccb4e9c1bcf259df53691dde88a9def0"

	if hash := getHash(voiceName, text); hash != expectedHash {
		t.Errorf("getHash(%q, %q) = %q, want %q", voiceName, text, hash, expectedHash)
	}
}

func TestHandleListLanguages(t *testing.T) {

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Create a Gin engine
	gin.SetMode(gin.TestMode)
	router := gin.Default()

	// Initialize Text-to-Speech client
	ttsClient, err := texttospeech.NewClient(context.Background())
	if err != nil {
		t.Fatalf("failed to create text-to-speech client: %v", err)
	}

	// Add the endpoint to the router
	router.GET("/listLanguages", handleListLanguages(ttsClient))

	// Create a mock request
	req, err := http.NewRequest(http.MethodGet, "/listLanguages", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Record the response
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Check the response status code
	if w.Code != http.StatusOK {
		t.Errorf("expected status OK; got %v", w.Code)
	}

	// Check the response body
	// This is where you would check that the response body contains the expected languages.
	// The exact check depends on the implementation of handleListLanguages and the data returned by the Text-to-Speech API.

	// fmt.Println(w.Body.String())
}

func TestGetVoice(t *testing.T) {
	tests := []struct {
		name      string
		voiceName string
		wantLang  string
		// wantVoiceType string
		wantErr bool
	}{
		{
			name:      "valid voice name",
			voiceName: "en-US-Neural2-A",
			wantLang:  "en-US",
			// wantVoiceType: "Neural2-A",
			wantErr: false,
		},
		{
			name:      "invalid voice name",
			voiceName: "invalid",
			wantLang:  "",
			// wantVoiceType: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLang, err := getLanguageCode(tt.voiceName)
			if (err != nil) != tt.wantErr {
				t.Errorf("getVoice() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotLang != tt.wantLang {
				t.Errorf("getVoice() gotLang = %v, want %v", gotLang, tt.wantLang)
			}
		})
	}
}

func TestSynthesizeSpeech(t *testing.T) {
	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		t.Fatalf("Error loading .env file: %v", err)
	}

	ctx := context.Background()

	// Initialize the Text-to-Speech client
	ttsClient, err := texttospeech.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to create Text-to-Speech client: %v", err)
	}

	// Call the synthesizeSpeech function
	voiceName := "fr-FR-Neural2-A"
	textNative := "Hello"
	audioContent, err := synthesizeSpeech(ttsClient, &TTSRequest{
		VoiceName:  voiceName,
		TextNative: textNative,
	})
	if err != nil {
		t.Fatalf("synthesizeSpeech() error = %v, want nil", err)
	}

	// Check that the function returned non-nil audio content
	if audioContent == nil {
		t.Error("synthesizeSpeech() audioContent = nil, want non-nil")
	}

	audioFolder := "test_audio/"
	// create the test_audio directory if it doesn't exist
	if _, err := os.Stat(audioFolder); os.IsNotExist(err) {
		if err := os.Mkdir(audioFolder, 0755); err != nil {
			t.Fatalf("Failed to create test_audio directory: %v", err)
		}
	}

	// Save the audio content to a local file in the test_audio directory
	localFilename := audioFolder + getHash(voiceName, textNative) + ".mp3"
	if err := os.WriteFile(localFilename, audioContent, 0644); err != nil {
		t.Fatalf("Failed to write audio content to local file: %v", err)
	}
}

func TestUploadAudio(t *testing.T) {
	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		t.Fatalf("Error loading .env file: %v", err)
	}

	ctx := context.Background()

	// Initialize the Text-to-Speech client
	ttsClient, err := texttospeech.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to create Text-to-Speech client: %v", err)
	}

	// Initialize the Storage client
	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to create Storage client: %v", err)
	}

	googleProjectId := os.Getenv("GOOGLE_PROJECT_ID")
	// Initialize the Firestore client
	fsClient, err := firestore.NewClient(ctx, googleProjectId)
	if err != nil {
		t.Fatalf("Failed to create Firestore client: %v", err)
	}

	// Get a handle to the Firebase Storage bucket
	bucketName := os.Getenv("FIREBASE_STORAGE_BUCKET")
	bucket := storageClient.Bucket(bucketName)

	// Generate fake audio content
	voiceName := "fr-FR-Neural2-A"
	textNative := "Hello!"
	filename := getHash(voiceName, textNative) + ".mp3"
	audioContent, err := synthesizeSpeech(ttsClient, &TTSRequest{
		VoiceName:  voiceName,
		TextNative: textNative,
	})
	if err != nil {
		t.Fatalf("synthesizeSpeech() error = %v, want nil", err)
	}

	// Call the fireBaseUploadAudio function
	docRef, data, err := fireBaseUploadAudio(fsClient, bucket, voiceName, textNative, filename, audioContent)
	if err != nil {
		t.Fatalf("fireBaseUploadAudio() error = %v, want nil", err)
	}

	fmt.Println(data)

	// Check that the document was created in Firestore
	snapshot, err := docRef.Get(ctx)
	if err != nil {
		t.Fatalf("Failed to get document: %v", err)
	}
	if !snapshot.Exists() {
		t.Error("Document does not exist, want exist")
	}
}
