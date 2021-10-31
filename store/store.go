package store

import (
	"encoding/binary"
	"math"
	"strconv"

	"github.com/dgraph-io/badger/v3"
	"github.com/pkg/errors"
)

var ErrUserIsFinished = errors.New("user is finished")

type Store struct {
	db *badger.DB
}

func New(path string) (*Store, error) {
	db, err := badger.Open(badger.DefaultOptions(path))
	if err != nil {
		return nil, err
	}

	return &Store{db: db}, nil
}

type Partfolio map[string]float64

func (s *Store) AddToPartfolio(userID int, secid string, percent float64) error {
	return s.db.Update(func(txn *badger.Txn) error {
		finished, err := s.isUserFinished(txn, userID)
		if err != nil {
			return err
		}
		if finished {
			return ErrUserIsFinished
		}

		key := getPartfolioPrefix(userID) + "_" + secid
		return txn.Set([]byte(key), float64ToBytes(percent))
	})
}

func (s *Store) IsUserFinished(userID int) (finished bool, err error) {
	err = s.db.View(func(txn *badger.Txn) error {
		finished, err = s.isUserFinished(txn, userID)
		return err
	})
	return finished, err
}

func (s *Store) Finish(userID int) error {
	return s.db.Update(func(txn *badger.Txn) error {
		finished, err := s.isUserFinished(txn, userID)
		if err != nil {
			return err
		}
		if finished {
			return ErrUserIsFinished
		}
		return txn.Set([]byte(getFinishedKey(userID)), []byte{})
	})
}

func (s *Store) GetPartfolio(userID int) (Partfolio, error) {
	partfolio := make(Partfolio)
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := getPartfolioPrefix(userID)
		bprefix := []byte(prefix)

		for it.Seek(bprefix); it.ValidForPrefix(bprefix); it.Next() {
			item := it.Item()
			k := item.Key()
			err := item.Value(func(v []byte) error {
				partfolio[string(k)] = bytesToFloat64(v)
				return nil
			})
			if err != nil {
				return err
			}
		}

		return nil
	})

	return partfolio, err
}

func (s *Store) isUserFinished(txn *badger.Txn, userID int) (bool, error) {
	_, err := txn.Get([]byte(getFinishedKey(userID)))

	if err != nil {
		if err == badger.ErrKeyNotFound {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func getFinishedKey(userID int) string {
	return strconv.Itoa(userID) + "_finished"
}

func getPartfolioPrefix(userID int) string {
	return strconv.Itoa(userID) + "_parfolio"
}

func bytesToFloat64(bytes []byte) float64 {
	bits := binary.LittleEndian.Uint64(bytes)
	float := math.Float64frombits(bits)
	return float
}

func float64ToBytes(float float64) []byte {
	bits := math.Float64bits(float)
	bytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(bytes, bits)
	return bytes
}
