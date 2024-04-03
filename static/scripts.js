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

                // Add move button
                const moveButton = document.createElement('button');
                moveButton.textContent = 'Move';
                moveButton.onclick = function() {
                    const newParentID = prompt('Enter ID of the new parent folder:');
                    if (newParentID) {
                        moveFile(file.id, newParentID);
                    }
                };
                li.appendChild(moveButton);

                // Add rename button
                const renameButton = document.createElement('button');
                renameButton.textContent = 'Rename';
                renameButton.onclick = function() {
                    const newName = prompt('Enter new name:');
                    if (newName) {
                        renameFile(file.id, newName);
                    }
                };
                li.appendChild(renameButton);

                // Add delete button
                const deleteButton = document.createElement('button');
                deleteButton.textContent = 'Delete';
                deleteButton.onclick = function() {
                    if (confirm('Are you sure you want to delete this file?')) {
                        deleteFile(file.id);
                    }
                };
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

function moveFile(fileID, newParentID) {
    fetch(`/move?id=${fileID}&parentID=${newParentID}`)
        .then(response => {
            if (response.ok) {
                console.log('File moved successfully');
                fetchFiles();
            } else {
                console.error('Failed to move file');
            }
        })
        .catch(error => console.error('Error moving file:', error));
}

function renameFile(fileID, newName) {
    fetch(`/rename?id=${fileID}&name=${encodeURIComponent(newName)}`)
        .then(response => {
            if (response.ok) {
                console.log('File renamed successfully');
                fetchFiles();
            } else {
                console.error('Failed to rename file');
            }
        })
        .catch(error => console.error('Error renaming file:', error));
}

function deleteFile(fileID) {
    fetch(`/delete?id=${fileID}`, { method: 'DELETE' })
        .then(response => {
            if (response.ok) {
                console.log('File deleted successfully');
                fetchFiles();
            } else {
                console.error('Failed to delete file');
            }
        })
        .catch(error => console.error('Error deleting file:', error));
}


window.onload = fetchFiles;
