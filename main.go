package main

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
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
var db *sql.DB

func init() {
	var err error
	// MySQL credentials and database name
	db, err = sql.Open("mysql", "root:password@tcp(localhost:3306)/KEYRING")
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}

	// Ensure the database connection is valid
	err = db.Ping()
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}

	// Create table to store folder IDs and passwords
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS folders (
                        id VARCHAR(255) PRIMARY KEY,
                        password VARCHAR(255)
                    )`)
	if err != nil {
		log.Fatalf("Error creating table: %v", err)
	}

	log.Println("Database initialized successfully")
}

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

		items, err := listItems("")
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to list items: %v", err), http.StatusInternalServerError)
			return
		}

		renderHTML(w, items)
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
		folderID := r.FormValue("folderID") // Get the folderID from the request
		_, err = uploadFile(fileName, file, folderID)
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

	// Modify "/folder" handler
	http.HandleFunc("/folder", func(w http.ResponseWriter, r *http.Request) {
		folderID := r.URL.Query().Get("id")

		// Check if the folder is locked
		if isFolderLocked(folderID) {
			if r.Method == http.MethodPost {
				password := r.FormValue("password")
				if unlockFolder(folderID, password) {
					// If password is correct, proceed to list items
					items, err := listItems(folderID)
					if err != nil {
						http.Error(w, fmt.Sprintf("Unable to list items: %v", err), http.StatusInternalServerError)
						return
					}
					renderHTML(w, items)
					return
				}
				// If password is incorrect, display an error message
				http.Error(w, "Incorrect password", http.StatusUnauthorized)
				return
			}

			// Set content type to HTML
			w.Header().Set("Content-Type", "text/html")

			// Render a form to enter the password
			fmt.Fprintf(w, `
				<form method="POST">
					<label for="password">Enter password to unlock folder:</label><br>
					<input type="password" id="password" name="password"><br>
					<input type="submit" value="Submit">
				</form>
			`)
			return
		}

		// If the folder is not locked, proceed to list items
		items, err := listItems(folderID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Unable to list items: %v", err), http.StatusInternalServerError)
			return
		}
		renderHTML(w, items)
	})

	http.HandleFunc("/setPassword", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			// Parse form data
			err := r.ParseForm()
			if err != nil {
				http.Error(w, "Failed to parse form data", http.StatusBadRequest)
				return
			}
	
			// Retrieve folder ID from URL query parameters
			folderID := r.URL.Query().Get("id")
	
			// Retrieve password from form data
			password := r.Form.Get("password")
	
			// Check if folder ID or password is empty
			if folderID == "" || password == "" {
				http.Error(w, "Folder ID or password cannot be empty", http.StatusBadRequest)
				return
			}
	
			// Check if folder already has a password set
			if isFolderLocked(folderID) {
				http.Error(w, "Folder already has a password set", http.StatusBadRequest)
				return
			}
	
			// Store folder ID and password in the database
			_, err = db.Exec("INSERT INTO folders (id, password) VALUES (?, ?)", folderID, password)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error setting password: %v", err), http.StatusInternalServerError)
				return
			}
	
			fmt.Fprintf(w, "Password set successfully for folder ID: %s", folderID)
			return
		}
	
		// Render form to set password
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `
			<html>
			<head><title>Set Password for Folder</title></head>
			<body>
			<h1>Set Password for Folder</h1>
			<form method="POST">
				<input type="hidden" id="folderID" name="folderID" value="%s">
				<label for="password">Password:</label><br>
				<input type="password" id="password" name="password"><br>
				<input type="submit" value="Set Password">
			</form>
			</body>
			</html>
		`, r.URL.Query().Get("id"))
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

	//http.HandleFunc("/createPassword", createPassword)

	log.Fatal(http.ListenAndServe(":8080", nil))
}

func listItems(parentID string) ([]*drive.File, error) {
	if parentID == "" {
		parentID = "root"
	}

	query := fmt.Sprintf("'%s' in parents", parentID)
	items, err := srv.Files.List().Q(query).Fields("files(id, name, mimeType)").Do()
	if err != nil {
		return nil, err
	}
	return items.Files, nil
}

func uploadFile(fileName string, file io.Reader, folderID string) (*drive.File, error) {
	driveFile := &drive.File{Name: fileName, Parents: []string{folderID}} // Set the parent folder ID
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

func isFolderLocked(folderID string) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM folders WHERE id = ?", folderID).Scan(&count)
	if err != nil {
		log.Println("Error checking folder lock:", err)
		return false // Assume folder is not locked if an error occurs
	}
	return count > 0 // If count is greater than 0, folder is locked
}

func unlockFolder(folderID, password string) bool {
	var storedPassword string
	err := db.QueryRow("SELECT password FROM folders WHERE id = ?", folderID).Scan(&storedPassword)
	if err != nil {
		return false // Folder not found or error retrieving password
	}
	return password == storedPassword
}

/*
func createPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		// Parse form data
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "Failed to parse form data", http.StatusBadRequest)
			return
		}

		// Retrieve folder ID and password from form data
		folderID := r.Form.Get("folderID")
		password := r.Form.Get("password")

		// Check if folder ID or password is empty
		if folderID == "" || password == "" {
			http.Error(w, "Folder ID or password cannot be empty", http.StatusBadRequest)
			return
		}

		// Check if folder already has a password set
		if isFolderLocked(folderID) {
			http.Error(w, "Folder already has a password set", http.StatusBadRequest)
			return
		}

		// Store folder ID and password in the database
		_, err = db.Exec("INSERT INTO folders (id, password) VALUES (?, ?)", folderID, password)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error setting password: %v", err), http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, "Password set successfully for folder ID: %s", folderID)
		return
	}

	// Render form to set password
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `
        <html>
        <head><title>Set Password for Folder</title></head>
        <body>
        <h1>Set Password for Folder</h1>
        <form method="POST">
            <label for="folderID">Folder ID:</label><br>
            <input type="text" id="folderID" name="folderID"><br>
            <label for="password">Password:</label><br>
            <input type="password" id="password" name="password"><br>
            <input type="submit" value="Set Password">
        </form>
        </body>
        </html>
    `)
}*/

func renderHTML(w http.ResponseWriter, items []*drive.File) {
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
		<div class="container1">
			<h1>UPLOAD FILE</h1>
			<input type="file" id="fileInput">
			<button onclick="uploadFile()">Upload</button>
		</div>

		<div class="container">
			<div>
				<h1>Folders in Google Drive</h1>
				<ul id="fileList">
					{{range .}}
					<li>
						{{if eq .MimeType "application/vnd.google-apps.folder"}}
							<a href="/folder?id={{.Id}}">{{.Name}}</a>
						{{end}}
					</li>
					{{end}}
				</ul>
			</div>

			<div>
				<h1>Files in Google Drive</h1>
				<ul id="fileList">
					{{range .}}
					<li>
						{{if and (not (eq .MimeType "application/vnd.google-apps.folder")) (not (eq .MimeType "application/vnd.google-apps.shortcut"))}}
						<a href="/download?id={{.Id}}">{{.Name}}</a>
						{{end}}
					</li>
					{{end}}
				</ul>
		</div>

		<script src="/static/scripts.js"></script>
	</body>

	</html>
	`
	tmpl := template.Must(template.New("index").Parse(html))
	tmpl.Execute(w, items)
}
