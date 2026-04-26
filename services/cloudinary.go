package services

import (
	"context"
	"os"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
)

func UploadImage(ctx context.Context, file interface{}, folder, publicID string) (string, error) {
	cld, err := cloudinary.NewFromURL(os.Getenv("CLOUDINARY_URL"))
	if err != nil {
		return "", err
	}

	resp, err := cld.Upload.Upload(ctx, file, uploader.UploadParams{
		PublicID:     publicID,
		Folder:       folder,
		ResourceType: "image",
	})
	if err != nil {
		return "", err
	}

	return resp.SecureURL, nil
}
