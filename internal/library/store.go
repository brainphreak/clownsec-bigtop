package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DB is everything Big Top remembers, saved as one JSON file in its data folder.
type DB struct {
	Libraries []*Library                       `json:"libraries"`
	Items     map[string]*Item                 `json:"items"`
	Shows     map[string]*Show                 `json:"shows"`
	Progress  map[string]map[string]*Progress  `json:"progress"` // console id -> item id -> place
}

// Store guards the DB and writes it back. Readers take RLock; anything that changes it takes
// Lock and then calls Dirty.
type Store struct {
	sync.RWMutex
	DB    DB
	path  string
	dirty chan struct{}
}

func OpenStore(dir string) (*Store, error) {
	s := &Store{path: filepath.Join(dir, "bigtop.json"), dirty: make(chan struct{}, 1)}
	s.DB = DB{Items: map[string]*Item{}, Shows: map[string]*Show{}, Progress: map[string]map[string]*Progress{}}
	if b, err := os.ReadFile(s.path); err == nil {
		if err := json.Unmarshal(b, &s.DB); err != nil {
			// keep the unreadable file for the user rather than overwrite it
			os.Rename(s.path, s.path+".broken-"+time.Now().Format("20060102-150405"))
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.DB.Items == nil {
		s.DB.Items = map[string]*Item{}
	}
	if s.DB.Shows == nil {
		s.DB.Shows = map[string]*Show{}
	}
	if s.DB.Progress == nil {
		s.DB.Progress = map[string]map[string]*Progress{}
	}
	go s.saver()
	return s, nil
}

// Dirty asks for a save. Writes are batched: at most one every few seconds.
func (s *Store) Dirty() {
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

func (s *Store) saver() {
	for range s.dirty {
		time.Sleep(3 * time.Second)
		s.Save()
	}
}

// Save writes the DB now (atomically: a temp file renamed over the old one).
func (s *Store) Save() error {
	s.RLock()
	b, err := json.MarshalIndent(&s.DB, "", " ")
	s.RUnlock()
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Lib finds a library by id (caller holds a lock).
func (s *Store) Lib(id string) *Library {
	for _, l := range s.DB.Libraries {
		if l.ID == id {
			return l
		}
	}
	return nil
}
