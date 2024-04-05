package main

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"

	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

var ctx = context.Background()
var srv *drive.Service
var db *sql.DB

func init() {
	var err error

    // Load environment variables from .env file
    if err := godotenv.Load(); err != nil {
        log.Fatalf("Error loading .env file: %v", err)
    }

    // Read environment variables
    dbUser := os.Getenv("DB_USER")
    dbPassword := os.Getenv("DB_PASSWORD")
    dbHost := os.Getenv("DB_HOST")
    dbName := os.Getenv("DB_NAME")

	// Construct database connection string
    dbDSN := fmt.Sprintf("%s:%s@tcp(%s)/%s", dbUser, dbPassword, dbHost, dbName)

    // Connect to MySQL database
    db, err = sql.Open("mysql", dbDSN)
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
	// Read environment variables
    clientID := os.Getenv("GOOGLE_CLIENT_ID")
    clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	redirectURL := os.Getenv("GOOGLE_REDIRECT_URL")

	// Set up OAuth2 configuration
	oauthConfig := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
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
				// fmt.Fprintf(w, `<p>Incorrect password</p>`)
				// fmt.Fprintf(w, `<script>setTimeout(function(){window.location.href='/folder?id=%s'}, 1000);</script>`, folderID)
				
				fmt.Fprintf(w, `
				<html>
				<head>
					<title>Incorrect Password Redirect</title>
					<link rel="stylesheet" href="/static/styles.css">
				</head>
				<body>
				<div>
					<form action="http://localhost:8080">
						<button type="submit">Home</button>
					</form>
				</div>
				<div class="container1">
					<p>Incorrect password</p>
					<script>
						setTimeout(function(){window.location.href='/folder?id=%s'}, 1000);
					</script>
				</div>
				</body>
				</html>
				`, folderID)


				return
			}

			// Set content type to HTML
			w.Header().Set("Content-Type", "text/html")

			// Render a form to enter the password
			fmt.Fprintf(w, `
			<html>
			<head>
				<title>Set Password for Folder</title>
				<link rel="stylesheet" href="/static/styles.css">
			</head>
			<body>
			<div>
				<form action="http://localhost:8080">
					<button type="submit">Home</button>
				</form>
			</div>
			<div class="container1">
				<form method="POST">
					<label for="password">Enter password to unlock folder:</label><br>
					<input type="password" id="password" name="password"><br>
					<input type="submit" value="Submit">
				</form>
			</div>
			</body>
			</html>
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

		// Retrieve folder ID from URL query parameters
		folderID := r.URL.Query().Get("id")
		if folderID == "" {
			http.Error(w, "Folder ID not provided", http.StatusBadRequest)
			return
		}

		isLocked := isFolderLocked(folderID)
		
		//If it's a GET request, render the form to set the password
		if r.Method == http.MethodGet {
			// Render form to set password
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `
				<html>
				<head>
					<title>Set Password for Folder</title>
					<link rel="stylesheet" href="/static/styles.css">
				</head>
				<body>
				<div>
					<form action="http://localhost:8080">
						<button type="submit">Home</button>
					</form>
				</div>
				<div class="container1">
					<h1>Set Password for Folder ID: %s</h1>
					<form method="POST" action="/setPassword?id=%s">
						<input type="hidden" id="folderID" name="folderID" value="%s">
						<label for="password">Password:</label><br>
						<input type="password" id="password" name="password"><br>
						<input type="submit" value="Set Password">
					</form>
				</div>
				</body>
				</html>
			`, folderID, folderID, folderID)
		}

		// Handle POST request to save the password
		if r.Method == http.MethodPost {
			// Parse form data
			err := r.ParseForm()
			if err != nil {
				http.Error(w, "Failed to parse form data", http.StatusBadRequest)
				return
			}

			// Retrieve password from form data
			password := r.FormValue("password")

			// Check if password is empty
			if password == "" {
				http.Error(w, "Password cannot be empty", http.StatusBadRequest)
				return
			}

			if isLocked {
				// Render confirmation prompt
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintf(w, `
				<script>
					const cnfstatus = confirm("Folder is already locked. Do you want to update the password?");
					if (!cnfstatus) {
						window.location.href = '/folder?id=%s';
					}
				</script>
				`, folderID)
			}

			// Insert password into the database
			if isLocked {
				updateQuery := "UPDATE `folders` SET `password` = ? WHERE `id` = ?"
				_, err := db.ExecContext(context.Background(), updateQuery, password, folderID)
				if err != nil {
					http.Error(w, fmt.Sprintf("Error updating password: %v", err), http.StatusInternalServerError)
					return
				}
			} else {
				query := "INSERT INTO `folders` (`id`, `password`) VALUES (?, ?)"
				_, err := db.ExecContext(context.Background(), query, folderID, password)
				if err != nil {
					http.Error(w, fmt.Sprintf("Error setting password: %v", err), http.StatusInternalServerError)
					return
				}
			}

			// w.Header().Set("Content-Type", "text/html")
			// fmt.Fprintf(w, "Password set successfully for folder ID: %s\n<script>setTimeout(function(){window.location.href='/'}, 1000);</script>", folderID)
			
			fmt.Fprintf(w, `
				<html>
				<head>
					<title>Successful Password Redirect</title>
					<link rel="stylesheet" href="/static/styles.css">
				</head>
				<body>
				<div>
					<form action="http://localhost:8080">
						<button type="submit">Home</button>
					</form>
				</div>
				<div class="container1">
					<p>Password set successfully for folder ID: %s</p>
					<script>
						setTimeout(function(){window.location.href='/'}, 1000);
					</script>
				</div>
				</body>
				</html>
			`, folderID)
			
			
			
			return
		}
	})

	http.HandleFunc("/removePassword", func(w http.ResponseWriter, r *http.Request) {
		folderID := r.FormValue("folderID")
	
		// Check if folderID is provided
		if folderID == "" {
			http.Error(w, "Folder ID not provided", http.StatusBadRequest)
			return
		}
	
		// Delete the folder from the database
		_, err := db.Exec("DELETE FROM folders WHERE id = ?", folderID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error deleting folder: %v", err), http.StatusInternalServerError)
			return
		}
	
		// Redirect to the home page ("/")
		http.Redirect(w, r, "/", http.StatusSeeOther)
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
		<div>
			<form action="http://localhost:8080">
				<button type="submit">Home</button>
			</form>
		</div>

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
						
							<p>{{.Name}}</p>
							<button onclick="window.location.href = 'https://drive.google.com/file/d/' + '{{.Id}}' + '/view'">View</button>
							<nsbp>
							<button onclick="window.location.href = '/download?id=' + '{{.Id}}'">Download</button>

						{{end}}
					</li>
					{{end}}
				</ul>
			</div>
		</div>

		<div class="container">
			<div>
				<button onclick="setPassword()">Set Password</button>
			</div>

			<div>
				<button onclick="removePassword()">Remove Password</button>
			</div>
		</div>

		<script src="/static/scripts.js"></script>
	</body>

	</html>
	`
	tmpl := template.Must(template.New("index").Parse(html))
	tmpl.Execute(w, items)
}
