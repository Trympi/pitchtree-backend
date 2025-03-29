package progress

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
)

type Tracker struct {
	channels map[string]chan string
	owners   map[string]string
	mu       sync.RWMutex
}

func NewTracker() *Tracker {
	return &Tracker{
		channels: make(map[string]chan string),
		owners:   make(map[string]string),
	}
}

// Add methods for managing progress channels...

func (t *Tracker) CreateChannel(id string, userID string) chan string {
	t.mu.Lock()
	defer t.mu.Unlock()

	log.Printf("Creating channel for deck %s, user %s", id, userID)
	ch := make(chan string, 10)
	t.channels[id] = ch
	t.owners[id] = userID
	return ch
}

func (t *Tracker) GetChannel(id string, userID string) (chan string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	log.Printf("Getting channel for deck %s, user %s", id, userID)
	ch, exists := t.channels[id]
	if !exists {
		log.Printf("Channel not found for deck %s", id)
		return nil, false
	}

	owner, ownerExists := t.owners[id]
	if !ownerExists || owner != userID {
		log.Printf("Owner mismatch: expected %s, got %s", owner, userID)
		return nil, false
	}

	return ch, true
}

func (t *Tracker) CloseChannel(id string) {
	log.Printf("close channel %s", id)
	t.mu.Lock()
	defer t.mu.Unlock()

	if ch, exists := t.channels[id]; exists {
		close(ch)
		delete(t.channels, id)
		delete(t.owners, id)
	}
}

type ProgressUpdate struct {
	Status      string `json:"status"`
	CurrentStep int    `json:"currentStep"`
	Message     string `json:"message"`
	DownloadUrl string `json:"downloadUrl,omitempty"`
	ViewUrl     string `json:"viewUrl,omitempty"`
}

func (t *Tracker) SendUpdate(id string, update ProgressUpdate) error {
	t.mu.RLock()
	ch, exists := t.channels[id]
	t.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no progress channel found for ID: %s", id)
	}

	data, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("failed to marshal update: %w", err)
	}

	ch <- string(data)
	return nil
}
