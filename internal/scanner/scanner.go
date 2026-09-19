package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/AdmGenSameer/gmove/internal/constants"
)

// Scanner scans the source directory for migration candidates.
type Scanner struct {
	sourceDir  string
	ignoreDirs map[string]bool
}

func New(sourceDir string) *Scanner {
	m := make(map[string]bool)
	for _, d := range constants.DefaultIgnoreDirs {
		m[strings.ToLower(d)] = true
	}
	return &Scanner{
		sourceDir:  filepath.Clean(sourceDir),
		ignoreDirs: m,
	}
}

func (s *Scanner) SetIgnoreDirs(dirs []string) {
	m := make(map[string]bool)
	for _, d := range dirs {
		m[strings.ToLower(d)] = true
	}
	s.ignoreDirs = m
}

// Scan discovers all media items (single files and movie directories) directly in the source directory.
func (s *Scanner) Scan() ([]*MediaItem, error) {
	entries, err := os.ReadDir(s.sourceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read source directory %s: %w", s.sourceDir, err)
	}

	var items []*MediaItem

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files and directories
		if strings.HasPrefix(name, ".") {
			continue
		}

		// Skip ignored directory names (e.g. Cloudbackup, Music, prowlarr)
		if entry.IsDir() && s.ignoreDirs[strings.ToLower(name)] {
			continue
		}

		fullPath := filepath.Join(s.sourceDir, name)

		if entry.IsDir() {
			item, err := s.scanDirectoryBundle(name, fullPath)
			if err != nil {
				continue
			}
			if item != nil {
				items = append(items, item)
			}
		} else {
			ext := strings.ToLower(filepath.Ext(name))
			if !constants.MediaExtensions[ext] || constants.IncompleteExtensions[ext] {
				continue
			}

			item, err := s.scanSingleFile(name, fullPath)
			if err != nil {
				continue
			}
			if item != nil {
				items = append(items, item)
			}
		}
	}

	// Sort alphabetically by name by default
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	return items, nil
}

func (s *Scanner) scanSingleFile(name, fullPath string) (*MediaItem, error) {
	fi, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}

	inode, dev, nlink := extractFileStats(fi)

	fileDetail := &FileDetail{
		RelativePath:  name,
		SourceAbsPath: fullPath,
		SizeBytes:     fi.Size(),
		ModTime:       fi.ModTime(),
		MtimeEpoch:    fi.ModTime().Unix(),
		Inode:         inode,
		DeviceID:      dev,
		Hardlinks:     nlink,
		IsMedia:       true,
	}

	item := &MediaItem{
		Name:          name,
		IsDirectory:   false,
		RelativePath:  name,
		SourceAbsPath: fullPath,
		SizeBytes:     fi.Size(),
		FileCount:     1,
		ModTime:       fi.ModTime(),
		MtimeEpoch:    fi.ModTime().Unix(),
		Inode:         inode,
		DeviceID:      dev,
		Hardlinks:     nlink,
		Files:         []*FileDetail{fileDetail},
	}

	return item, nil
}

func (s *Scanner) scanDirectoryBundle(dirName, dirPath string) (*MediaItem, error) {
	var files []*FileDetail
	var totalSize int64
	var latestMtime int64
	var maxNlink uint64
	hasMedia := false

	err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			// Skip hidden subdirectories
			if strings.HasPrefix(d.Name(), ".") && path != dirPath {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if constants.IncompleteExtensions[ext] {
			return nil
		}

		isMedia := constants.MediaExtensions[ext]
		isSidecar := constants.SidecarExtensions[ext]

		// Only include media or recognized sidecars
		if !isMedia && !isSidecar {
			return nil
		}

		if isMedia {
			hasMedia = true
		}

		fi, err := d.Info()
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(s.sourceDir, path)
		if err != nil {
			return nil
		}

		inode, dev, nlink := extractFileStats(fi)
		if nlink > maxNlink {
			maxNlink = nlink
		}

		if fi.ModTime().Unix() > latestMtime {
			latestMtime = fi.ModTime().Unix()
		}

		totalSize += fi.Size()

		files = append(files, &FileDetail{
			RelativePath:  relPath,
			SourceAbsPath: path,
			SizeBytes:     fi.Size(),
			ModTime:       fi.ModTime(),
			MtimeEpoch:    fi.ModTime().Unix(),
			Inode:         inode,
			DeviceID:      dev,
			Hardlinks:     nlink,
			IsMedia:       isMedia,
		})

		return nil
	})

	if err != nil || !hasMedia || len(files) == 0 {
		return nil, nil
	}

	dirInfo, err := os.Stat(dirPath)
	var dirInode, dirDev uint64
	if err == nil {
		dirInode, dirDev, _ = extractFileStats(dirInfo)
	}

	return &MediaItem{
		Name:          dirName,
		IsDirectory:   true,
		RelativePath:  dirName,
		SourceAbsPath: dirPath,
		SizeBytes:     totalSize,
		FileCount:     len(files),
		MtimeEpoch:    latestMtime,
		Inode:         dirInode,
		DeviceID:      dirDev,
		Hardlinks:     maxNlink,
		Files:         files,
	}, nil
}

func extractFileStats(fi os.FileInfo) (inode uint64, dev uint64, nlink uint64) {
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(stat.Ino), uint64(stat.Dev), uint64(stat.Nlink)
	}
	return 0, 0, 1
}

// Filter searches items by substring (case-insensitive) in name or relative path.
func Filter(items []*MediaItem, query string) []*MediaItem {
	if query == "" {
		return items
	}
	q := strings.ToLower(query)
	var matched []*MediaItem
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Name), q) || strings.Contains(strings.ToLower(item.RelativePath), q) {
			matched = append(matched, item)
		}
	}
	return matched
}

// SortBy specifies sorting field.
type SortBy string

const (
	SortByName SortBy = "name"
	SortBySize SortBy = "size"
	SortByDate SortBy = "date"
)

// Sort sorts items by specified criterion.
func Sort(items []*MediaItem, sortBy SortBy, descending bool) {
	sort.Slice(items, func(i, j int) bool {
		var less bool
		switch sortBy {
		case SortBySize:
			less = items[i].SizeBytes < items[j].SizeBytes
		case SortByDate:
			less = items[i].MtimeEpoch < items[j].MtimeEpoch
		default:
			less = strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		}
		if descending {
			return !less
		}
		return less
	})
}
