package docker

// Handle building docker images for grading.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/pkg/archive"

	"github.com/edulinq/autograder/internal/common"
	"github.com/edulinq/autograder/internal/config"
	"github.com/edulinq/autograder/internal/log"
	"github.com/edulinq/autograder/internal/util"
)

const (
	TEMPDIR_PREFIX = "autograder-docker-build-"
)

type BuildOptions struct {
	Rebuild bool `help:"Rebuild images ignoring caches." default:"false"`
}

func NewBuildOptions() *BuildOptions {
	return &BuildOptions{
		Rebuild: false,
	}
}

func BuildImage(imageSource ImageSource) error {
	return BuildImageWithOptions(imageSource, NewBuildOptions())
}

func BuildImageWithOptions(imageSource ImageSource, options *BuildOptions) error {
	imageInfo := imageSource.GetImageInfo()
	leaveBuildDir := config.KEEP_BUILD_DIRS.Get()

	tempDir, err := util.MkDirTempFull(TEMPDIR_PREFIX+imageInfo.Name+"-", !leaveBuildDir)
	if err != nil {
		return fmt.Errorf("Failed to create temp build directory for '%s': '%w'.", imageInfo.Name, err)
	}

	if leaveBuildDir {
		log.Debug("Leaving behind image building dir.", imageSource, log.NewAttr("path", tempDir))
	} else {
		defer util.RemoveDirent(tempDir)
	}

	err = writeDockerContext(imageInfo, tempDir)
	if err != nil {
		return err
	}

	// Don't remove build artifacts when testing (it slows down tests).
	removeBuildArtifacts := !config.UNIT_TESTING_MODE.Get()

	buildOptions := types.ImageBuildOptions{
		Tags:        []string{imageInfo.Name},
		Dockerfile:  "Dockerfile",
		Remove:      removeBuildArtifacts,
		ForceRemove: removeBuildArtifacts,
	}

	if options.Rebuild {
		buildOptions.NoCache = true
	}

	// Create the build context by adding all the relevant files.
	tar, err := archive.TarWithOptions(tempDir, &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("Failed to create tar build context for image '%s': '%w'.", imageInfo.Name, err)
	}

	return buildImage(imageSource, buildOptions, tar)
}

func buildImage(imageSource ImageSource, buildOptions types.ImageBuildOptions, tar io.ReadCloser) error {
	docker, err := getDockerClient()
	if err != nil {
		return err
	}
	defer docker.Close()

	response, err := docker.ImageBuild(context.Background(), tar, buildOptions)
	if err != nil {
		return fmt.Errorf("Failed to run docker image build command: '%w'.", err)
	}

	output, err := collectBuildOutput(imageSource, response)
	log.Trace("Image Build Output", imageSource, log.NewAttr("image-build-output", output), err)
	if err != nil {
		return fmt.Errorf("Found error(s) in Docker build output: '%w'.", err)
	}

	return nil
}

// Try to get the build output from a build response.
// Note that the response may be from a failure.
func collectBuildOutput(imageSource ImageSource, response types.ImageBuildResponse) (string, error) {
	if response.Body == nil {
		return "", nil
	}

	defer response.Body.Close()

	output := strings.Builder{}
	var errs error = nil

	responseScanner := bufio.NewScanner(response.Body)
	for responseScanner.Scan() {
		line := responseScanner.Text()

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		jsonData, err := util.JSONMapFromString(line)
		if err != nil {
			output.WriteString("<WARNING: The following output line was not JSON.>")
			output.WriteString(line)
		}

		rawText, ok := jsonData["error"]
		if ok {
			text, ok := rawText.(string)
			if !ok {
				text = "<ERROR: Docker output JSON value is not a string.>"
			}

			text = strings.TrimSpace(text)
			if text != "" {
				errs = errors.Join(errs, fmt.Errorf(text))
			}
		}

		rawText, ok = jsonData["stream"]
		if ok {
			text, ok := rawText.(string)
			if !ok {
				text = "<ERROR: Docker output JSON value is not a string.>"
			}

			output.WriteString(text)
		}
	}

	err := responseScanner.Err()
	if err != nil {
		errs = errors.Join(errs, fmt.Errorf("Failed to scan docker image build response: '%w'.", err))
	}

	return output.String(), errs
}

// Write a full docker build context (Dockerfile and static files) to the given directory.
func writeDockerContext(imageInfo *ImageInfo, outDir string) error {
	_, _, workDir, err := common.CreateStandardGradingDirs(outDir)
	if err != nil {
		return fmt.Errorf("Could not create standard grading directories: '%w'.", err)
	}

	// Copy over the static files (and do any file ops).
	sourceBaseDir, sourceContainmentDir := imageInfo.BaseDirFunc()
	err = util.CopyFileSpecsWithOps(sourceBaseDir, sourceContainmentDir, workDir, workDir, outDir,
		imageInfo.StaticFiles, imageInfo.PreStaticFileOperations, imageInfo.PostStaticFileOperations)
	if err != nil {
		return fmt.Errorf("Failed to copy static imageInfo files: '%w'.", err)
	}

	dockerConfigPath := filepath.Join(outDir, DOCKER_CONFIG_FILENAME)
	err = util.ToJSONFile(imageInfo.GetGradingConfig(), dockerConfigPath)
	if err != nil {
		return fmt.Errorf("Failed to create docker config file: '%w'.", err)
	}

	dockerPostSubmittionOpsPath := filepath.Join(outDir, DOCKER_POST_SUBMISSION_OPS_FILENAME)
	err = writePostSubmissionOpsScript(imageInfo, dockerPostSubmittionOpsPath)
	if err != nil {
		return fmt.Errorf("Failed to write post-submission operations script: '%w'.", err)
	}

	dockerfilePath := filepath.Join(outDir, "Dockerfile")
	err = writeDockerfile(imageInfo, workDir, dockerfilePath)
	if err != nil {
		return err
	}

	return nil
}

// Write out the post-submission ops as a shell script.
func writePostSubmissionOpsScript(imageInfo *ImageInfo, path string) error {
	var lines []string

	lines = append(lines, "#!/bin/bash\n")

	lines = append(lines, fmt.Sprintf("# Post-Submission operations for '%s'.\n", imageInfo.Name))

	for _, op := range imageInfo.PostSubmissionFileOperations {
		lines = append(lines, op.ToUnixForDocker("."))
	}

	err := util.WriteFile(strings.Join(lines, "\n"), path)
	if err != nil {
		return err
	}

	return nil
}

func writeDockerfile(imageInfo *ImageInfo, workDir string, path string) error {
	contents, err := toDockerfile(imageInfo, workDir)
	if err != nil {
		return fmt.Errorf("Failed get contenets for dockerfile ('%s'): '%w'.", path, err)
	}

	err = util.WriteFile(contents, path)
	if err != nil {
		return fmt.Errorf("Failed write dockerfile ('%s'): '%w'.", path, err)
	}

	return nil
}

func toDockerfile(imageInfo *ImageInfo, workDir string) (string, error) {
	// Note that we will insert blank lines for formatting.
	lines := make([]string, 0)

	lines = append(lines, fmt.Sprintf("FROM %s", imageInfo.Image), "")

	// Ensure standard directories are created.
	lines = append(lines, "# Core directories")
	for _, dir := range []string{DOCKER_BASE_DIR, DOCKER_INPUT_DIR, DOCKER_OUTPUT_DIR, DOCKER_WORK_DIR, DOCKER_SCRIPTS_DIR} {
		lines = append(lines, fmt.Sprintf("RUN mkdir -p '%s'", dir))
	}
	lines = append(lines, "")

	// Set the working directory.
	lines = append(lines, fmt.Sprintf("WORKDIR %s", DOCKER_BASE_DIR), "")

	// Copy over the config and post-op script files.
	lines = append(lines, fmt.Sprintf("COPY %s %s", DOCKER_CONFIG_FILENAME, DOCKER_CONFIG_PATH), "")
	lines = append(lines, fmt.Sprintf("COPY %s %s", DOCKER_POST_SUBMISSION_OPS_FILENAME, DOCKER_POST_SUBMISSION_OPS_PATH), "")

	// Append pre-static docker commands.
	lines = append(lines, "# Pre-Static Commands")
	lines = append(lines, imageInfo.PreStaticDockerCommands...)
	lines = append(lines, "")

	// Copy over all the contents of the work directory (this is after post-static file ops).
	dirents, err := os.ReadDir(workDir)
	if err != nil {
		return "", fmt.Errorf("Failed to list work dir ('%s') for static files: '%w'.", workDir, err)
	}

	lines = append(lines, "# Static Files")
	for _, dirent := range dirents {
		sourcePath := DockerfilePathQuote(filepath.Join(common.GRADING_WORK_DIRNAME, dirent.Name()))
		destPath := DockerfilePathQuote(filepath.Join(DOCKER_WORK_DIR, dirent.Name()))

		lines = append(lines, fmt.Sprintf("COPY %s %s", sourcePath, destPath))
	}
	lines = append(lines, "")

	// Append post-static docker commands.
	lines = append(lines, "# Post-Static Commands")
	lines = append(lines, imageInfo.PostStaticDockerCommands...)
	lines = append(lines, "")

	// Add an invoation (CMD) if it exists.
	if len(imageInfo.Invocation) > 0 {
		parts := make([]string, 0, len(imageInfo.Invocation))
		for _, part := range imageInfo.Invocation {
			parts = append(parts, DockerfilePathQuote(part))
		}

		lines = append(lines, "# Invocation")
		lines = append(lines, fmt.Sprintf("CMD [%s]", strings.Join(parts, ", ")))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n"), nil
}
