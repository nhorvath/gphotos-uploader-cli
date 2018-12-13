package fileshandling

import (
	"fmt"
	imageLib "image"

	// register decoders for jpeg and png
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"net/http"
	"os"

	"github.com/Nr90/imgsim"
	"github.com/palantir/stacktrace"
	"github.com/steakknife/hamming"
	photoslibrary "google.golang.org/api/photoslibrary/v1"
)

var (
	deletionsChan = make(chan DeletionJob)
)

type DeletionJob struct {
	uploadedMediaItem *photoslibrary.MediaItem
	localFilePath     string
}

func QueueDeletionJob(uploadedMediaItem *photoslibrary.MediaItem, localImgPath string) {
	deletionsChan <- DeletionJob{uploadedMediaItem, localImgPath}
}

func CloseDeletionsChan() { close(deletionsChan) }

func StartDeletionsWorker() (doneDeleting chan struct{}) {
	doneDeleting = make(chan struct{})
	go func() {
		for deletionJob := range deletionsChan {
			deletionJob.deleteIfCorrectlyUploaded()
		}
		doneDeleting <- struct{}{}
	}()
	return doneDeleting
}

func imageFromPath(filePath string) (imageLib.Image, error) {
	reader, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	img, _, err := imageLib.Decode(reader)
	if err != nil {
		return nil, err
	}

	return img, nil
}

func imageFromURL(URL string) (imageLib.Image, error) {
	res, err := http.Get(URL)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("expected http status 200, got %d", res.StatusCode)
	}

	img, _, err := imageLib.Decode(res.Body)
	if err != nil {
		return nil, err
	}

	return img, nil
}

func isSameImage(upImg, localImg imageLib.Image) bool {
	upDHash := imgsim.DifferenceHash(upImg).String()
	localDHash := imgsim.DifferenceHash(localImg).String()

	if len(upDHash) != len(localDHash) {
		return false
	}
	hammingDistance := hamming.Strings(upDHash, localDHash)

	if hammingDistance < len(upDHash)/16 {
		return true
	}
	return false
}

// isImageCorrectlyUploaded checks that the image that was uploaded is visually similar to the local one, before deleting the local one
func isImageCorrectlyUploaded(uploadedMediaItem *photoslibrary.MediaItem, localImgPath string) (bool, error) {
	// TODO: add sameness check for videos (use file hash) and delete if same
	if !IsImage(localImgPath) {
		return false, fmt.Errorf("%s is not an image. won't delete local file", localImgPath)
	}

	// compare uploaded image and local one
	upImg, err := imageFromURL(uploadedMediaItem.BaseUrl)
	if err != nil {
		return false, stacktrace.Propagate(err, "failed getting image from URL")
	}
	localImg, err := imageFromPath(localImgPath)
	if err != nil {
		return false, stacktrace.Propagate(err, "failed loading local image from path")
	}

	if isSameImage(upImg, localImg) {
		return true, nil
	}

	return false, nil
}

func (deletionJob *DeletionJob) deleteIfCorrectlyUploaded() {
	isImageCorrectlyUploaded, err := isImageCorrectlyUploaded(deletionJob.uploadedMediaItem, deletionJob.localFilePath)
	if err != nil {
		fmt.Printf("%s. Won't delete\n", err)
		return
	}

	if isImageCorrectlyUploaded {
		fmt.Printf("uploaded file %s was checked for integrity. Will now delete.\n", deletionJob.localFilePath)
		if err = os.Remove(deletionJob.localFilePath); err != nil {
			fmt.Println("failed deleting file")
		}
		return
	} else {
		fmt.Println("not the same image. Won't delete")
		return
	}
}
