package manager

import (
	"context"
	"errors"
)

var (
	ErrDuplicate    = errors.New("asset already exists")
	ErrNotFound     = errors.New("asset not found")
	ErrObjectExists = errors.New("object already exists")
)

type Variant struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"`
	Key    string `json:"key"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
}

type Asset struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	MIMEType    string    `json:"mimeType"`
	Checksum    string    `json:"checksum"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	Alt         string    `json:"alt"`
	Caption     string    `json:"caption"`
	TakenAt     string    `json:"takenAt,omitempty"`
	Location    string    `json:"location,omitempty"`
	Copyright   string    `json:"copyright,omitempty"`
	Tags        []string  `json:"tags"`
	ArticleRefs []string  `json:"articleRefs"`
	Variants    []Variant `json:"variants"`
	CreatedAt   string    `json:"createdAt"`
	UpdatedAt   string    `json:"updatedAt"`
}

type MetadataUpdate struct {
	Alt         string   `json:"alt"`
	Caption     string   `json:"caption"`
	TakenAt     string   `json:"takenAt"`
	Location    string   `json:"location"`
	Copyright   string   `json:"copyright"`
	Tags        []string `json:"tags"`
	ArticleRefs []string `json:"articleRefs"`
}

type Object struct {
	Key         string
	Body        []byte
	ContentType string
	Metadata    map[string]string
}

type ObjectStore interface {
	Put(context.Context, Object) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, []string) error
	URL(string) string
}

type ImageTransformer interface {
	TransformWebP(context.Context, string, int) ([]byte, error)
}
