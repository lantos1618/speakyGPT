package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

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
}
