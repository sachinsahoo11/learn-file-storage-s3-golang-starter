package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxVideoSize = 1 << 30
	http.MaxBytesReader(w, r.Body, maxVideoSize)

	videoID := r.PathValue("videoID")
	videoUUID, err := uuid.Parse(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to parse video id", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	videoMetadata, err := cfg.db.GetVideo(videoUUID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't get video metadata", err)
		return
	}

	if videoMetadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "you don't own this video", err)
		return
	}

	file, headers, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't parse video", err)
		return
	}

	defer file.Close()

	mediaType := headers.Header.Get("Content-Type")
	extension := strings.Split(mediaType, "/")[1]

	parsedMediaType, _, err := mime.ParseMediaType(mediaType)

	if parsedMediaType != "video/mp4" {
		respondWithError(w, http.StatusInternalServerError, "media type is not right", err)
		return
	}

	tmpFile, err := os.CreateTemp("", "tubely-upload")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't create temp dir", err)
		return
	}

	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	io.Copy(tmpFile, file)
	aspectRatio, err := getVideoAspectRatio(tmpFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to get asapect ratio", err)
		return
	}

	var folder string
	switch aspectRatio {
	case "16:9":
		folder = "landscape"
	case "9:16":
		folder = "portrait"
	default:
		folder = "other"
	}

	tmpFile.Seek(0, io.SeekStart)

	byteVideoID := make([]byte, 10)
	rand.Read(byteVideoID)
	stringVideoID := base64.URLEncoding.EncodeToString(byteVideoID)

	videoKey := folder + "/" + stringVideoID + "." + extension
	log.Println(videoKey)

	_, err = cfg.s3Client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &videoKey,
		Body:        tmpFile,
		ContentType: &parsedMediaType,
	})

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to upload video to s3", err)
		return
	}

	videoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, videoKey)

	videoMetadata.VideoURL = &videoURL
	err = cfg.db.UpdateVideo(videoMetadata)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't update videoURL", err)
		return
	}
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	streamBuffer := &bytes.Buffer{}
	cmd.Stdout = streamBuffer
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	type streamData struct {
		Streams []struct {
			DisplayAspectRatio string `json:"display_aspect_ratio,omitempty"`
		} `json:"streams"`
	}

	var obj streamData
	err = json.Unmarshal(streamBuffer.Bytes(), &obj)
	if err != nil {
		return "", err
	}

	return obj.Streams[0].DisplayAspectRatio, nil
}
