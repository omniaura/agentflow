package iterfs

import (
	"io/fs"
	"iter"
	"os"

	"github.com/rs/zerolog/log"
)

type DirSeq iter.Seq2[string, fs.DirEntry]

func NewDir(file *os.File) DirSeq {
	dir, err := file.ReadDir(0)
	if err != nil {
		log.Err(err).
			Str("dir", file.Name()).
			Msg("Error reading directory")
		return nil
	}
	return func(yield func(string, fs.DirEntry) bool) {
		for _, entry := range dir {
			y, err := recursiveOpenfile(file.Name(), entry, yield)
			if !y {
				break
			}
			if err != nil {
				log.Err(err).Msg("Error opening file")
				return
			}
		}
	}
}

func recursiveOpenfile(rootName string, entry fs.DirEntry, yield func(string, fs.DirEntry) bool) (bool, error) {
	if !entry.IsDir() {
		rootName = rootName + "/" + entry.Name()
		log.Trace().Str("name", rootName).Msg("Yielding file")
		if !yield(rootName, entry) {
			return false, nil
		}
		return true, nil
	}
	log.Trace().Str("name", rootName).Msg("Recursing")
	rootName = rootName + "/" + entry.Name()
	file, err := os.Open(rootName)
	if err != nil {
		return false, err
	}
	dir, err := file.ReadDir(0)
	if err != nil {
		return false, err
	}
	for _, entry := range dir {
		rootName := rootName + "/" + entry.Name()
		if entry.IsDir() {
			recursiveOpenfile(rootName, entry, yield)
			continue
		}
		log.Trace().Str("name", rootName).Msg("Yielding file")
		if !yield(rootName, entry) {
			return false, nil
		}
	}
	return true, nil
}
