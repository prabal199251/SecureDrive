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
    const currentFolderID = urlParams.get('id');

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

window.onload = fetchFiles;
