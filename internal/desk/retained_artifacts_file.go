package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
)

const retainedArtifactDirectory = ".desk-private/retained-artifacts"

type fileRetainedArtifactRepository struct {
	open      func(bool) (*os.Root, error)
	authorize func(context.Context) error
}

func (f fileRetainedArtifactRepository) WithAuthority(authorize func(context.Context) error) retainedArtifactRepository {
	f.authorize = authorize
	return f
}

func (f fileRetainedArtifactRepository) current(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.authorize != nil {
		return f.authorize(ctx)
	}
	return nil
}

// The caller retains the pinned project's lifetime. Features receive the same
// repository boundary without reconstructing artifact storage paths themselves.
func retainedArtifactsForProject(project *os.Root) retainedArtifactRepository {
	return fileRetainedArtifactRepository{open: func(create bool) (*os.Root, error) { return openRetainedArtifactsWithin(project, create) }}
}
func (s *Server) openRetainedArtifacts(create bool) (*os.Root, error) {
	return openRetainedArtifactsWithin(s.root, create)
}
func openRetainedArtifactsWithin(project *os.Root, create bool) (*os.Root, error) {
	root, made, err := openOwnFolder(project, strings.Split(retainedArtifactDirectory, "/"), create, "retained artifacts")
	if err == nil && made {
		err = writePrivateData(root, ".gitignore", []byte("*\n"))
		if err != nil {
			root.Close()
		}
	}
	return root, err
}
func (f fileRetainedArtifactRepository) Get(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	if err := f.current(ctx); err != nil {
		return nil, err
	}
	if !ref.valid() {
		return nil, errRetainedArtifactChanged
	}
	root, err := f.open(false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	raw, err := readPrivateData(root, ref.SHA256, ref.Bytes)
	if err == nil {
		err = verifyRetainedArtifact(ref, raw)
	}
	if err == nil {
		err = f.current(ctx)
	}
	if err != nil {
		return nil, err
	}
	return raw, nil
}
func (f fileRetainedArtifactRepository) Put(ctx context.Context, raw []byte) (retainedArtifactRef, error) {
	if err := f.current(ctx); err != nil {
		return retainedArtifactRef{}, err
	}
	if len(raw) > retainedArtifactByteLimit {
		return retainedArtifactRef{}, errors.New("retained artifact exceeds its storage limit")
	}
	ref := artifactReference(raw)
	root, err := f.open(true)
	if err != nil {
		return retainedArtifactRef{}, err
	}
	defer root.Close()
	lock, err := root.Open(".")
	if err != nil {
		return retainedArtifactRef{}, err
	}
	defer lock.Close()
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		return retainedArtifactRef{}, err
	}
	if f.authorize != nil {
		if err = f.current(ctx); err != nil {
			return retainedArtifactRef{}, err
		}
	}
	prior, err := readPrivateData(root, ref.SHA256, retainedArtifactByteLimit)
	if err == nil {
		if !bytes.Equal(raw, prior) {
			return retainedArtifactRef{}, errRetainedArtifactChanged
		}
		if f.authorize != nil {
			if err = f.current(ctx); err != nil {
				return retainedArtifactRef{}, err
			}
		}
		return ref, nil // exact repeated content needs no write
	}
	if !errors.Is(err, os.ErrNotExist) {
		return retainedArtifactRef{}, err
	}
	if err = f.current(ctx); err != nil {
		return retainedArtifactRef{}, err
	}
	// Directory exclusion covers the absence check and staged publication. The
	// shared primitive syncs and reads back the file before acknowledging it.
	if err = writePrivateData(root, ref.SHA256, raw); err != nil {
		return retainedArtifactRef{}, err
	}
	if f.authorize != nil {
		if err = f.current(ctx); err != nil {
			return retainedArtifactRef{}, err
		}
	}
	return ref, nil
}
