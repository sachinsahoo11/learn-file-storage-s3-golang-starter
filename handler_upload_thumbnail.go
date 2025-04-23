package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)
	file, headers, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't parse thumbnail", err)
		return
	}

	mediaType := headers.Header.Get("Content-Type")
	extension := strings.Split(mediaType, "/")[1]

	parsedMediaType, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error while parsing media type", err)
		return
	}

	if parsedMediaType != "image/jpeg" && parsedMediaType != "image/png" {
		respondWithError(w, http.StatusInternalServerError, "invalid file type", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "you can't change this video's thumbnail", err)
		return
	}

	byteTnID := make([]byte, 10)
	rand.Read(byteTnID)
	stringTnID := base64.URLEncoding.EncodeToString(byteTnID)

	tnFilePath := filepath.Join(cfg.assetsRoot, stringTnID+"."+extension)
	log.Println(tnFilePath)
	writeFile, err := os.Create(tnFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed to create file", err)
		return
	}

	io.Copy(writeFile, file)
	absFilePath := fmt.Sprintf("http://localhost:%s/%s", cfg.port, tnFilePath)

	video.ThumbnailURL = &absFilePath
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't update thumbnail to video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
