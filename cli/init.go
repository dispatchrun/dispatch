package cli

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const (
	githubTarballURL = "https://github.com/%s/tarball/main"
	githubAPIURL     = "https://api.github.com/repos/%s/branches/main"
	repo             = "dispatchrun/dispatch-templates"
	dispatchUserDir  = "dispatch"
)

func directoryExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func isDirectoryEmpty(path string) (bool, error) {
	dir, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer dir.Close()

	// Read directory names, limiting to one to check if it's not empty
	_, err = dir.Readdirnames(1)
	if err == nil {
		// The directory is not empty
		return false, nil
	}
	if err == io.EOF {
		// The directory is empty
		return true, nil
	}
	// Some other error occurred
	return true, err
}

func downloadAndExtractTemplates(destDir string) error {
	url := fmt.Sprintf(githubTarballURL, repo)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download templates: %s", resp.Status)
	}

	return extractTarball(resp.Body, destDir)
}

func extractTarball(r io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	var topLevelDir string

	for {
		header, err := tr.Next()
		switch {
		case err == io.EOF:
			return nil
		case err != nil:
			return err
		case header == nil:
			continue
		}

		// We need to strip the top-level directory from the file paths
		// It contains the repository name and the commit SHA which we don't need
		// Get the top-level directory name
		if topLevelDir == "" {
			parts := strings.Split(header.Name, "/")
			if len(parts) > 1 {
				topLevelDir = parts[0]
			}
		}

		// Strip the top-level directory from the file path
		relPath := strings.TrimPrefix(header.Name, topLevelDir+"/")
		target := filepath.Join(destDir, relPath)

		// fmt.Printf("Extracting to %s\n", target)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tr); err != nil {
				file.Close()
				return err
			}
			file.Close()
		}
	}
}

func getAppDataDir(appName string) (string, error) {
	var configDir string
	var err error

	switch runtime.GOOS {
	case "windows":
		configDir, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	case "darwin":
		configDir, err = os.UserConfigDir()
		if err != nil {
			return "", err
		}
	default: // "linux" and other Unix-like systems
		configDir = os.Getenv("XDG_CONFIG_HOME")
		if configDir == "" {
			configDir, err = os.UserConfigDir()
			if err != nil {
				return "", err
			}
		}
	}

	appDataDir := filepath.Join(configDir, appName)
	err = os.MkdirAll(appDataDir, 0755)
	if err != nil {
		return "", err
	}

	return appDataDir, nil
}

func getLatestCommitSHA(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get latest commit SHA: %s", resp.Status)
	}

	var result struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return "", err
	}

	return result.Commit.SHA, nil
}

func readDirectories(path string) ([]string, error) {
	files, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	var directories []string
	for _, file := range files {
		if file.IsDir() {
			directories = append(directories, file.Name())
		}
	}

	return directories, nil
}

func cleanDirectory(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()

	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}

	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return err
		}
	}

	return nil
}

func copyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Construct the destination path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			// Create the directory
			if err := os.MkdirAll(dstPath, info.Mode()); err != nil {
				return err
			}
		} else {
			// Copy the file
			if err := copyFile(path, dstPath); err != nil {
				return err
			}
		}
		return nil
	})
}

func copyFile(srcFile string, dstFile string) error {
	src, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(dstFile)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}

	// Copy file permissions
	srcInfo, err := os.Stat(srcFile)
	if err != nil {
		return err
	}
	return os.Chmod(dstFile, srcInfo.Mode())
}

func initCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "init <template> [path]",
		Short:   "Initialize a new Dispatch project",
		GroupID: "dispatch",
		RunE: func(cmd *cobra.Command, args []string) error {
			// get or create the Dispatch templates directory
			dispatchUserDirPath, err := getAppDataDir(dispatchUserDir)
			if err != nil {
				fmt.Printf("failed to get Dispatch templates directory: %s", err)
			}

			// well-known paths for Dispatch templates
			dispatchTemplatesDirPath := filepath.Join(dispatchUserDirPath, "templates")
			dispatchTemplatesHashPath := filepath.Join(dispatchUserDirPath, "templates.sha")

			// read the latest commit SHA
			sha, err := os.ReadFile(dispatchTemplatesHashPath)
			if err != nil {
				if !os.IsNotExist(err) {
					cmd.SilenceUsage = true
					cmd.PrintErrf("failed to read templates SHA: %s", err)
				}
			}

			// get the latest commit SHA from the templates repository
			url := fmt.Sprintf(githubAPIURL, repo)
			remoteSHA, err := getLatestCommitSHA(url)
			if err != nil {
				cmd.Printf("failed to get latest commit SHA: %v", err)
			}

			// update the templates if the latest commit SHA is different
			if remoteSHA != "" && string(sha) != remoteSHA {
				cmd.Printf("Downloading templates update...\n")
				err = downloadAndExtractTemplates(dispatchTemplatesDirPath)
				if err != nil {
					cmd.Printf("failed to download and extract templates: %v", err)
				} else {
					cmd.Print("Templates have been updated\n\n")
					// TODO: possible improvement:
					// find which templates have been added/removed/modified
					// and/or
					// show last n commit messages as changes
				}

				// save the latest commit SHA
				err = os.WriteFile(dispatchTemplatesHashPath, []byte(remoteSHA), 0644)
				if err != nil {
					cmd.Printf("failed to save templates SHA: %v", err)
				}
			}

			// read the available templates
			templates, err := readDirectories(dispatchTemplatesDirPath)

			if err != nil {
				cmd.SilenceUsage = true
				if os.IsNotExist(err) {
					cmd.PrintErrf("templates directory does not exist in %s. Please run `dispatch init` to download the templates", dispatchTemplatesDirPath)
				}
				cmd.PrintErrf("failed to read templates directory. : %s", err)
			}

			if len(templates) == 0 {
				cmd.SilenceUsage = true
				return fmt.Errorf("templates directory %s is corrupted. Please clean it and try again", dispatchTemplatesDirPath)
			}

			var templatesList string = ""

			for _, template := range templates {
				templatesList += "  " + template + "\n"
			}
			cmd.SetUsageTemplate(cmd.UsageTemplate() + "\nAvailable templates:\n" + templatesList)

			// if no arguments are provided (user wants to download/update templates only), print the usage
			if len(args) == 0 {
				cmd.Print(cmd.UsageString())
				return nil
			}

			var directory string
			var exists = true

			wantedTemplate := args[0]
			isTemplateFound := false

			// find template in the available templates
			for _, template := range templates {
				if template == wantedTemplate {
					isTemplateFound = true
					break
				}
			}

			if !isTemplateFound {
				cmd.SilenceUsage = true
				cmd.Printf("Template %s is not supported.\n\nAvailable templates:\n %s", wantedTemplate, templatesList)
				return nil
			}

			// check if a directory is provided
			if len(args) > 1 {
				directory = args[1]
				flag, err := directoryExists(directory)
				exists = flag

				if err != nil {
					cmd.SilenceUsage = true
					return fmt.Errorf("failed to check if directory exists: %w", err)
				}

				// create the directory if it doesn't exist
				if !exists {
					err := os.MkdirAll(directory, 0755)
					if err != nil {
						cmd.SilenceUsage = true
						return fmt.Errorf("failed to create directory %v: %w", directory, err)
					}
					exists = true
				}
			} else {
				directory = "."
			}

			// check if the if directory exists and is empty
			if exists {
				isEmpty, err := isDirectoryEmpty(directory)
				if err != nil {
					cmd.SilenceUsage = true
					return fmt.Errorf("failed to check if directory is empty: %w", err)
				}
				if !isEmpty {
					cmd.Printf("Directory %v is not empty. Do you want to overwrite it? [y/N]\n", directory)

					var response string
					fmt.Scanln(&response)

					if strings.ToLower(response) == "y" {
						err := cleanDirectory(directory)
						if err != nil {
							cmd.SilenceUsage = true
							return fmt.Errorf("failed to clean directory: %w", err)
						}
					} else {
						return nil
					}
				}
			}
			path, err := filepath.Abs(directory)
			if err != nil {
				cmd.SilenceUsage = true
				return fmt.Errorf("failed to get absolute path: %w", err)
			}

			cmd.Printf("Template %s was created in %s\n", wantedTemplate, path)

			// copy the template to the destination
			err = copyDir(filepath.Join(dispatchTemplatesDirPath, wantedTemplate), path)
			if err != nil {
				cmd.SilenceUsage = true
				return fmt.Errorf("failed to copy template: %w", err)
			}

			return nil
		},
	}

	return cmd
}
