package model

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

type Checkpoint struct {
	Consumer  string `gorm:"primaryKey"`
	Key       string `gorm:"primaryKey"`
	Value     []byte
	CreatedAt time.Time `selector:"metadata.creationTimestamp"`
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (tv Checkpoint) String() string {
	val, _ := json.Marshal(tv)
	return string(val)
}
