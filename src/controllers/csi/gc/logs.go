package csigc

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/afero"
)

const (
	maxLogFolderSizeBytes = 300000
	maxNumberOfLogFiles   = 1000
	maxLogAge             = 14 * 24 * time.Hour
)

type logFileInfo struct {
	UnusedVolumeIDs []os.FileInfo
	NumberOfFiles   int64
	OverallSize     int64
}

func (gc *CSIGarbageCollector) runLogGarbageCollection(tenantUUID string) {
	logs, err := gc.getLogFileInfo(tenantUUID)
	if err != nil {
		log.Info("failed to get log file information")
		return
	}

	gc.removeLogsIfNecessary(logs, maxLogFolderSizeBytes, maxNumberOfLogFiles, tenantUUID)
}

func (gc *CSIGarbageCollector) removeLogsIfNecessary(logs *logFileInfo, maxSize int64, maxFile int64, tenantUUID string) {
	shouldDelete := logs.NumberOfFiles > 0 && (logs.OverallSize > maxSize || logs.NumberOfFiles > maxFile)
	if shouldDelete {
		gc.tryRemoveLogFolders(logs.UnusedVolumeIDs, tenantUUID)
	}
}

func (gc *CSIGarbageCollector) getLogFileInfo(tenantUUID string) (*logFileInfo, error) {
	unusedVolumeIDs, err := gc.getUnusedVolumeIDs(tenantUUID)
	if err != nil {
		return nil, err
	}

	var nrOfFiles int64
	var size int64
	for _, volumeID := range unusedVolumeIDs {
		_ = walkDirectory(gc.fs, gc.path.OverlayVarDir(tenantUUID, volumeID.Name()), func(_ string, file os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !file.IsDir() {
				nrOfFiles++
				size += file.Size()
			}
			return nil
		})
	}

	return &logFileInfo{
		UnusedVolumeIDs: unusedVolumeIDs,
		NumberOfFiles:   nrOfFiles,
		OverallSize:     size,
	}, nil
}

func walkDirectory(fs afero.Fs, rootPath string, walkFn filepath.WalkFunc) []error {
	queue := []string{rootPath}
	errors := make([]error, 0)

	for len(queue) > 0 {
		var path string
		path, queue = pop(queue)
		fileInfos, err := afero.ReadDir(fs, path)

		if err != nil {
			errors = append(errors, err)
		}

		for _, fileInfo := range fileInfos {
			filePath := filepath.Join(path, fileInfo.Name())

			if fileInfo.IsDir() {
				queue = append(queue, filePath)
			}

			err = walkFn(filePath, fileInfo, err)

			if err != nil {
				errors = append(errors, err)
			}
		}
	}

	return errors
}

func pop(queue []string) (string, []string) {
	return queue[0], queue[1:]
}

func (gc *CSIGarbageCollector) getUnusedVolumeIDs(tenantUUID string) ([]os.FileInfo, error) {
	var unusedVolumeIDs []os.FileInfo

	volumeIDs, err := afero.ReadDir(gc.fs, gc.path.AgentRunDir(tenantUUID))
	if err != nil {
		return nil, err
	}

	for _, volumeID := range volumeIDs {
		dirEntry, err := os.ReadDir(gc.path.OverlayMappedDir(tenantUUID, volumeID.Name()))
		if err != nil {
			return nil, err
		}

		if len(dirEntry) <= 0 {
			unusedVolumeIDs = append(unusedVolumeIDs, volumeID)
		}
	}

	return unusedVolumeIDs, nil
}

func (gc *CSIGarbageCollector) tryRemoveLogFolders(unusedVolumeIDs []os.FileInfo, tenantUUID string) {
	for _, unusedVolumeID := range unusedVolumeIDs {
		if isOlderThanTwoWeeks(unusedVolumeID.ModTime()) {
			if err := gc.fs.RemoveAll(gc.path.AgentRunDirForVolume(tenantUUID, unusedVolumeID.Name())); err != nil {
				log.Info("failed to remove logs for pod", "podUID", unusedVolumeID.Name(), "error", err)
			}
		}
	}
}

func isOlderThanTwoWeeks(t time.Time) bool {
	return time.Since(t) > maxLogAge
}
