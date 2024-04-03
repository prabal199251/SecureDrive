package main

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

var ctx = context.Background()
var srv *drive.Service

func main() {
	// Set up OAuth2 configuration
	oauthConfig := &oauth2.Config{
		ClientID:     "707223796998-csu5jm55tohljkubhd0oq5cckomhtqcm.apps.googleusercontent.com",
		ClientSecret: "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",
		RedirectURL:  "http://localhost:8080/oauth2callback",
		Scopes:       []string{drive.DriveScope},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/auth",
			TokenURL: "https://accounts.google.com/o/oauth2/token",
		},
	}

	// HTTP Handlers
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if srv == nil {
			http.Redirect(w, r, oauthConfig.AuthCodeURL("state-token", oauth2.AccessTypeOffline), http.StatusTemporaryRedirect)
			return
		}

		folders, err := listFolders()
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to list folders: %v", err), http.StatusInternalServerError)
			return
		}

		renderHTML(w, folders)
	})

	http.HandleFunc("/move", func(w http.ResponseWriter, r *http.Request) {
		fileID := r.URL.Query().Get("id")
		newParentID := r.URL.Query().Get("parentID")
		err := moveFile(fileID, newParentID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to move file: %v", err), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "File moved successfully")
	})

	http.HandleFunc("/rename", func(w http.ResponseWriter, r *http.Request) {
		fileID := r.URL.Query().Get("id")
		newName := r.URL.Query().Get("name")
		err := renameFile(fileID, newName)
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to rename file: %v", err), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "File renamed successfully")
	})

	http.HandleFunc("/delete", func(w http.ResponseWriter, r *http.Request) {
		fileID := r.URL.Query().Get("id")
		err := deleteFile(fileID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to delete file: %v", err), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "File deleted successfully")
	})

	http.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to get file: %v", err), http.StatusBadRequest)
			return
		}
		defer file.Close()

		fileName := header.Filename
		_, err = uploadFile(fileName, file)
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to upload file: %v", err), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	http.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		fileID := r.URL.Query().Get("id")
		if fileID == "" {
			http.Error(w, "File ID not provided", http.StatusBadRequest)
			return
		}

		fileName, err := downloadFile(fileID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to download file: %v", err), http.StatusInternalServerError)
			return
		}

		http.ServeFile(w, r, fileName)
	})

	http.HandleFunc("/oauth2callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		token, err := oauthConfig.Exchange(ctx, code)
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to retrieve token: %v", err), http.StatusInternalServerError)
			return
		}

		httpClient := oauthConfig.Client(ctx, token)
		srv, err = drive.NewService(ctx, option.WithHTTPClient(httpClient))
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to create Drive service: %v", err), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	log.Fatal(http.ListenAndServe(":8080", nil))
}

func listFiles() ([]*drive.File, error) {
	files, err := srv.Files.List().Fields("files(id, name)").Do()
	if err != nil {
		return nil, err
	}
	return files.Files, nil
}

func listFolders() ([]*drive.File, error) {
	query := "mimeType='application/vnd.google-apps.folder'"
	folders, err := srv.Files.List().Q(query).Fields("files(id, name)").Do()
	if err != nil {
		return nil, err
	}
	return folders.Files, nil
}

func uploadFile(fileName string, file io.Reader) (*drive.File, error) {
	driveFile := &drive.File{Name: fileName}
	_, err := srv.Files.Create(driveFile).Media(file).Do()
	if err != nil {
		return nil, err
	}
	return driveFile, nil
}

func downloadFile(fileID string) (string, error) {
	file, err := srv.Files.Get(fileID).Fields("name").Do()
	if err != nil {
		return "", err
	}

	resp, err := srv.Files.Get(fileID).Download()
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	out, err := os.Create(file.Name)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", err
	}

	return file.Name, nil
}

func moveFile(fileID, newParentID string) error {
	_, err := srv.Files.Update(fileID, nil).AddParents(newParentID).RemoveParents("").Do()
	return err
}

func renameFile(fileID, newName string) error {
	file := &drive.File{Name: newName}
	_, err := srv.Files.Update(fileID, file).Fields("name").Do()
	return err
}

func deleteFile(fileID string) error {
	err := srv.Files.Delete(fileID).Do()
	return err
}

func renderHTML(w http.ResponseWriter, files []*drive.File) {
	html := `
	<!DOCTYPE html>
	<html>
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>Google Drive API Example</title>
		<link rel="stylesheet" href="/static/styles.css">
		<script src="/static/scripts.js"></script>
	</head>
	<body>
    <div class="container">
        <h1>Files in Google Drive</h1>
        <ul id="fileList">
            {{range .}}
            <li>
                <a href="/download?id={{.Id}}">{{.Name}}</a>
                <form action="/move" method="post">
                    <input type="hidden" name="id" value="{{.Id}}">
                    <label for="parentID">Move to folder:</label>
                    <input type="text" id="parentID" name="parentID" placeholder="Enter parent folder ID">
                    <button type="submit">Move</button>
                </form>
                <form action="/rename" method="post">
                    <input type="hidden" name="id" value="{{.Id}}">
                    <label for="newName">New name:</label>
                    <input type="text" id="newName" name="newName" placeholder="Enter new name">
                    <button type="submit">Rename</button>
                </form>
                <form action="/delete" method="post">
                    <input type="hidden" name="id" value="{{.Id}}">
                    <button type="submit">Delete</button>
                </form>
            </li>
            {{end}}
        </ul>
        <h2>Upload File</h2>
        <input type="file" id="fileInput">
        <button onclick="uploadFile()">Upload</button>
    </div>
    <script src="/static/scripts.js"></script>
</body>

	</html>
	`
	tmpl := template.Must(template.New("index").Parse(html))
	tmpl.Execute(w, files)
}
