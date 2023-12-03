// TTW Software Team
// Mathis Van Eetvelde
// 2021-present

// Modified by Aditya Karnam
// 2021
// Added file overwrite support

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sethvargo/go-githubactions"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const (
	filenameInput                 = "filename"
	nameInput                     = "name"
	folderIdInput                 = "folderId"
	credentialsInput              = "credentials"
	overwriteInput                = "false"
	mimeTypeInput                 = "mimeType"
	useCompleteSourceName         = "useCompleteSourceFilenameAsName"
	mirrorDirectoryStructureInput = "mirrorDirectoryStructureInput"
	namePrefixInput               = "namePrefix"
)

func uploadToDrive(svc *drive.Service, filename string, folderId string, driveFile *drive.File, name string, mimeType string) error {
	fi, err := os.Lstat(filename)
	if err != nil {
		return fmt.Errorf("unable to stat file: %w", err)
	}

	if fi.IsDir() {
		githubactions.Infof("%s is a directory. skipping upload.", filename)
		return nil
	}

	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("opening file with filename: %s failed with error: %w", filename, err)
	}

	if driveFile != nil {
		f := &drive.File{
			Name:     name,
			MimeType: mimeType,
		}
		_, err = svc.Files.Update(driveFile.Id, f).AddParents(folderId).Media(file).SupportsAllDrives(true).Do()
	} else {
		f := &drive.File{
			Name:     name,
			MimeType: mimeType,
			Parents:  []string{folderId},
		}
		_, err = svc.Files.Create(f).Media(file).SupportsAllDrives(true).Do()
	}

	if err != nil {
		return fmt.Errorf("uploading file with filename: %s failed with error: %w", filename, err)
	}

	githubactions.Debugf("uploaded/updated file.")
	return nil
}

func main() {
	// get filename argument from action input
	filename := githubactions.GetInput(filenameInput)
	if filename == "" {
		missingInput(filenameInput)
	}

	files, err := filepath.Glob(filename)
	githubactions.Infof("files: %v", files)
	if err != nil {
		githubactions.Fatalf(fmt.Sprintf("Invalid filename pattern: %s", err))
	}

	if len(files) == 0 {
		githubactions.Fatalf(fmt.Sprintf("No file found! pattern: %s", filename))
	}

	// get overwrite flag
	var overwriteFlag bool
	overwrite := githubactions.GetInput(overwriteInput)
	if overwrite == "" {
		githubactions.Warningf("%s is disabled.", overwriteInput)
		overwriteFlag = false
	} else {
		overwriteFlag, _ = strconv.ParseBool(overwrite)
	}

	// get name argument from action input
	name := githubactions.GetInput(nameInput)

	// get folderId argument from action input
	folderId := githubactions.GetInput(folderIdInput)
	if folderId == "" {
		missingInput(folderIdInput)
	}

	// get file mimeType argument from action input
	mimeType := githubactions.GetInput(mimeTypeInput)

	var useCompleteSourceFilenameAsNameFlag bool
	useCompleteSourceFilenameAsName := githubactions.GetInput(useCompleteSourceName)
	if useCompleteSourceFilenameAsName == "" {
		githubactions.Infof("%s is disabled.", useCompleteSourceName)
		useCompleteSourceFilenameAsNameFlag = false
	} else {
		useCompleteSourceFilenameAsNameFlag, _ = strconv.ParseBool(useCompleteSourceFilenameAsName)
	}

	var mirrorDirectoryStructureFlag bool
	mirrorDirectoryStructure := githubactions.GetInput(mirrorDirectoryStructureInput)
	if mirrorDirectoryStructure == "" {
		githubactions.Infof("%s is disabled.", mirrorDirectoryStructureInput)
		mirrorDirectoryStructureFlag = false
	} else {
		mirrorDirectoryStructureFlag, _ = strconv.ParseBool(mirrorDirectoryStructure)
	}

	// get filename prefix
	filenamePrefix := githubactions.GetInput(namePrefixInput)

	// get base64 encoded credentials argument from action input
	credentials := githubactions.GetInput(credentialsInput)
	if credentials == "" {
		missingInput(credentialsInput)
	}

	// add base64 encoded credentials argument to mask
	githubactions.AddMask(credentials)

	// decode credentials to []byte
	decodedCredentials, err := base64.StdEncoding.DecodeString(credentials)
	if err != nil {
		githubactions.Fatalf(fmt.Sprintf("base64 decoding of 'credentials' failed with error: %v", err))
	}

	// add decoded credentials argument to mask
	creds := strings.TrimSuffix(string(decodedCredentials), "\n")
	githubactions.AddMask(creds)

	// instantiating a new drive service
	ctx := context.Background()
	svc, err := drive.NewService(ctx, option.WithCredentialsJSON([]byte(creds)))
	if err != nil {
		githubactions.Errorf("creating drive client failed with error: %s", err)
	}

	useSourceFilename := len(files) > 1

	// Save the folderId because it might get overwritten by createDriveDirectory
	originalFolderId := folderId
	for _, file := range files {
		folderId = originalFolderId

		githubactions.Infof("Processing file %s", file)
		if mirrorDirectoryStructureFlag {
			directoryStructure := strings.Split(filepath.Dir(file), string(os.PathSeparator))
			githubactions.Infof("Mirroring directory structure: %v", directoryStructure)
			for _, dir := range directoryStructure {
				folderId, err = createDriveDirectory(svc, folderId, dir)
				if err != nil {
					githubactions.Fatalf("creating directory %s failed with error: %s", dir, err)
				}
			}
		}

		targetName := name
		if useCompleteSourceFilenameAsNameFlag {
			targetName = file
		} else if useSourceFilename || name == "" {
			targetName = filepath.Base(file)
		}

		if targetName == "" {
			githubactions.Fatalf("could not discover target file name")
		}

		if filenamePrefix != "" {
			targetName = filenamePrefix + targetName
		}

		if err := uploadFile(svc, file, folderId, targetName, mimeType, overwriteFlag); err != nil {
			githubactions.Fatalf("uploading file failed with error: %s", err)
		}
	}
}

func createDriveDirectory(svc *drive.Service, folderId string, name string) (string, error) {
	githubactions.Infof("Checking for existing folder %s", name)
	r, err := svc.Files.
		List().
		Fields("files(name,id,mimeType,parents)").
		Q("name='" + name + "'" + " and mimeType='application/vnd.google-apps.folder'").
		IncludeItemsFromAllDrives(true).
		Corpora("allDrives").
		SupportsAllDrives(true).
		Do()
	if err != nil {
		return "", fmt.Errorf("unable to retrieve files: %w", err)
	}

	foundFolders := 0
	var nextFolderId string
	for _, i := range r.Files {
		for _, p := range i.Parents {
			if p == folderId {
				foundFolders++
				githubactions.Infof("Found existing folder %s.", name)
				nextFolderId = i.Id
			}
		}
	}

	if foundFolders == 0 {
		githubactions.Infof("Creating folder: %s", name)
		f := &drive.File{
			Name:     name,
			MimeType: "application/vnd.google-apps.folder",
			Parents:  []string{folderId},
		}

		d, err := svc.Files.Create(f).Fields("id").SupportsAllDrives(true).Do()
		if err != nil {
			return "", fmt.Errorf("creating folder failed with error: %w", err)
		}

		nextFolderId = d.Id
	}

	return nextFolderId, nil
}

func uploadFile(svc *drive.Service, filename string, folderId string, name string, mimeType string, overwriteFlag bool) error {
	githubactions.Infof("target file name: %s", name)

	if !overwriteFlag {
		return uploadToDrive(svc, filename, folderId, nil, name, mimeType)
	}

	// overwrite flag is true
	r, err := svc.Files.
		List().
		Fields("files(name,id,mimeType,parents)").
		Q("name='" + name + "'").
		IncludeItemsFromAllDrives(true).
		Corpora("allDrives").
		SupportsAllDrives(true).
		IncludeTeamDriveItems(true).
		Do()
	if err != nil {
		return fmt.Errorf("unable to retrieve files: %w", err)
	}

	githubactions.Infof("Files: %d", len(r.Files))
	var currentFile *drive.File = nil
	for _, i := range r.Files {
		found := false
		if name == i.Name {
			currentFile = i
			for _, p := range i.Parents {
				if p == folderId {
					githubactions.Debugf("file found in expected folder")
					found = true
					break
				}
			}
		}

		if found {
			break
		}
	}

	if currentFile == nil {
		githubactions.Infof("No similar files found. Creating a new file")
		return uploadToDrive(svc, filename, folderId, nil, name, mimeType)
	}

	githubactions.Infof("Overwriting file: %s (%s)", currentFile.Name, currentFile.Id)
	return uploadToDrive(svc, filename, folderId, currentFile, name, mimeType)
}

func missingInput(inputName string) {
	githubactions.Fatalf(fmt.Sprintf("missing input '%v'", inputName))
}
