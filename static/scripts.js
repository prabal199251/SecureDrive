function fetchFiles() {
    fetch('/files')
        .then(response => response.json())
        .then(data => {
            const fileList = document.getElementById('fileList');
            fileList.innerHTML = '';
            data.forEach(file => {
                const li = document.createElement('li');
                const link = document.createElement('a');
                link.href = `/download?id=${file.id}`;
                link.textContent = file.name;
                link.setAttribute('download', '');
                li.appendChild(link);

                li.appendChild(deleteButton);

                fileList.appendChild(li);
            });
        })
        .catch(error => console.error('Error fetching files:', error));
}


function uploadFile() {
    const fileInput = document.getElementById('fileInput');
    const file = fileInput.files[0];
    const formData = new FormData();
    formData.append('file', file);

    // Get the current folder ID from the URL
    const urlParams = new URLSearchParams(window.location.search);
    currentFolderID = urlParams.get('id');

    if(currentFolderID==null) {
        currentFolderID="._.";
    }

    // Append the current folder ID to the upload request
    formData.append('folderID', currentFolderID);
    fetch('/upload', {
        method: 'POST',
        body: formData
    })
    .then(response => {
        if (response.ok) {
            console.log('File uploaded successfully');
            fetchFiles();
        } else {
            console.error('Failed to upload file');
        }
    })
    .catch(error => console.error('Error uploading file:', error));
}


function setPassword() {
    const urlParams = new URLSearchParams(window.location.search);
    const folderID = urlParams.get('id');
    if (folderID) {
        window.location.href = `/setPassword?id=${folderID}`;
    }
}

function removePassword() {
    const urlParams = new URLSearchParams(window.location.search);
    const formData = new FormData();
    const folderID = urlParams.get('id');
    if (folderID) {
        formData.append('folderID', folderID);

        fetch('/removePassword', {
            method: 'POST',
            body: formData
        })
        // fetch(`/removePassword?id=${folderID}`)
            .then(response => {
                if (response.ok) {
                    console.log('Password removed successfully');

                } else {
                    console.error('Failed to remove password');
                }
            })
            .catch(error => console.error('Error removing password:', error));
    }
}

window.onload = fetchFiles;
