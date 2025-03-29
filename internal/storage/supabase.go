package storage

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	storage "github.com/supabase-community/storage-go"
)

type SupabaseStorage struct {
	client  *storage.Client
	baseURL string
}

func NewSupabaseStorage() (*SupabaseStorage, error) {
	supabaseURL := os.Getenv("SUPABASE_URL")
	supabaseKey := os.Getenv("SUPABASE_SERVICE_KEY")

	if supabaseURL == "" || supabaseKey == "" {
		return nil, fmt.Errorf("supabase credentials not set")
	}

	client := storage.NewClient(supabaseURL+"/storage/v1", supabaseKey, nil)

	return &SupabaseStorage{
		client:  client,
		baseURL: supabaseURL,
	}, nil
}

func (s *SupabaseStorage) UploadFile(filePath, bucketName, fileName string) (string, error) {
	// Read the file
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	log.Println("upload our file:", bucketName, fileName)

	// Detect MIME type
	contentType := mime.TypeByExtension(filepath.Ext(fileName))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Upload to Supabase Storage
	_, err = s.client.UploadFile(
		bucketName,
		fileName,
		bytes.NewReader(fileContent),
		storage.FileOptions{ContentType: &contentType},
	)
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	// Generate public URL
	publicURL := fmt.Sprintf("%s/storage/v1/object/public/%s/%s",
		strings.TrimSuffix(s.baseURL, "/"),
		bucketName,
		fileName)

	return publicURL, nil
}

func (s *SupabaseStorage) DownloadFile(url string, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file, status: %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to save file: %w", err)
	}

	return nil
}
