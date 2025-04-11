package webhook

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestWebhook(t *testing.T) {
	// for manual tests
	t.Skip()
	sigChannel := make(chan os.Signal, 1)
	signal.Notify(sigChannel, os.Interrupt, syscall.SIGTERM)

	server := &http.Server{
		Addr:         ":3333",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	http.HandleFunc("/webhook", getNotification)
	fmt.Printf("webhook listener started\n")

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("server error: %v\n", err)
		}
	}()

	<-sigChannel
	fmt.Printf("Shutting down server...\n")
	if err := server.Close(); err != nil {
		fmt.Printf("Error closing server: %v\n", err)
	}
}

func getNotification(resp http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(resp, "Method not allowed", http.StatusMethodNotAllowed)
		fmt.Printf("Not a post request!\n")
		return
	}

	res, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(resp, "Internal server error", http.StatusInternalServerError)
		fmt.Printf("notification read error: %v\n", err)
		return
	}
	_ = req.Body.Close()
	fmt.Printf("Notification: %s\n", res)
	resp.WriteHeader(http.StatusOK)
}
