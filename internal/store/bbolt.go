package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"agent-container-hub/internal/model"
	"go.etcd.io/bbolt"
)

var (
	sessionBucket  = []byte("sessions")
	buildJobBucket = []byte("build_jobs")
)

type BoltStore struct {
	db *bbolt.DB
}

func Open(path string) (*BoltStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		for _, bucket := range [][]byte{sessionBucket, buildJobBucket} {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create bucket: %w", err)
	}
	return &BoltStore{db: db}, nil
}

func (s *BoltStore) SaveSession(_ context.Context, session *model.Session) error {
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	return s.put(sessionBucket, session.ID, payload)
}

func (s *BoltStore) GetSession(_ context.Context, id string) (*model.Session, error) {
	var session *model.Session
	err := s.get(sessionBucket, id, &session)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (s *BoltStore) ListSessions(_ context.Context) ([]*model.Session, error) {
	sessions := make([]*model.Session, 0)
	err := s.list(sessionBucket, func(value []byte) error {
		var record model.Session
		if err := json.Unmarshal(value, &record); err != nil {
			return err
		}
		sessions = append(sessions, &record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *BoltStore) DeleteSession(_ context.Context, id string) error {
	return s.delete(sessionBucket, id)
}

func (s *BoltStore) SaveBuildJob(_ context.Context, job *model.BuildJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal build job: %w", err)
	}
	return s.put(buildJobBucket, job.ID, payload)
}

func (s *BoltStore) ListBuildJobs(_ context.Context, environmentName string) ([]*model.BuildJob, error) {
	jobs := make([]*model.BuildJob, 0)
	err := s.list(buildJobBucket, func(value []byte) error {
		var record model.BuildJob
		if err := json.Unmarshal(value, &record); err != nil {
			return err
		}
		if environmentName != "" && record.EnvironmentName != environmentName {
			return nil
		}
		jobs = append(jobs, &record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *BoltStore) Close() error {
	return s.db.Close()
}

func (s *BoltStore) put(bucket []byte, key string, payload []byte) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucket).Put([]byte(key), payload)
	})
}

func (s *BoltStore) get(bucket []byte, key string, destination any) error {
	return s.db.View(func(tx *bbolt.Tx) error {
		payload := tx.Bucket(bucket).Get([]byte(key))
		if payload == nil {
			return ErrNotFound
		}
		return json.Unmarshal(payload, destination)
	})
}

func (s *BoltStore) list(bucket []byte, visit func([]byte) error) error {
	return s.db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucket).ForEach(func(_, value []byte) error {
			return visit(value)
		})
	})
}

func (s *BoltStore) delete(bucket []byte, key string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bucket).Delete([]byte(key))
	})
}
