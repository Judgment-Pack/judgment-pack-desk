package desk

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
)

// The repository is scoped to one authorized workspace at construction. Callers
// pass collection identities, never paths or provider credentials. CompareAndSwap
// is the only mutation: an empty revision means create-only; otherwise exactly
// that saved snapshot must still exist. A failed/uncertain write is not retried.
// Read/TryRead guard a consistent collection view until Close. Adapters must
// prevent concurrent replacement of that view while an admission uses it; an
// MVCC snapshot alone is insufficient. Callers must close the view and must not
// recursively enter the repository while holding it. No callback is retried.
type inputPreparationRepository interface {
	List(context.Context) ([]inputPreparationSummary, error)
	Get(context.Context, string) (inputPreparationReply, error)
	Read(context.Context) (inputPreparationRead, error)
	TryRead(context.Context) (inputPreparationRead, error)
	CompareAndSwap(context.Context, inputPreparation, string) (inputPreparationReply, error)
}

type inputPreparationSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Records   int    `json:"records"`
	UpdatedAt string `json:"updatedAt"`
}

var (
	errInputPreparationConflict     = errors.New("dialect collection revision changed")
	errInputPreparationCapacity     = errors.New("dialect collection capacity reached")
	errInputPreparationBusy         = errors.New("dialect collection storage busy")
	errInputPreparationStoreMissing = errors.New("dialect collection store does not exist")
)

const inputPreparationCountLimit = 100

type fileInputPreparationRepository struct {
	open func(create bool) (*os.Root, error)
}

func (s *Server) preparationRepository() inputPreparationRepository {
	if s.inputPreparations != nil {
		return s.inputPreparations
	}
	return fileInputPreparationRepository{open: s.openInputPreparations}
}

func (f fileInputPreparationRepository) locked(ctx context.Context, write bool, use func(*os.Root) error) error {
	view, err := f.openTransaction(ctx, write, true)
	if err != nil {
		return err
	}
	defer view.Close()
	return use(view.root)
}

func inputPreparationNames(root *os.Root) ([]string, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		key := strings.TrimSuffix(entry.Name(), ".json")
		if strings.HasSuffix(entry.Name(), ".json") && inputPreparationID.MatchString(key) {
			names = append(names, key)
		}
	}
	return names, nil
}

func (f fileInputPreparationRepository) List(ctx context.Context) ([]inputPreparationSummary, error) {
	items := []inputPreparationSummary{}
	err := f.locked(ctx, false, func(root *os.Root) error {
		names, err := inputPreparationNames(root)
		if err != nil {
			return err
		}
		for _, id := range names {
			if err = ctx.Err(); err != nil {
				return err
			}
			saved, err := readInputPreparation(root, id)
			if err != nil {
				return err // unreadable collections are never reported as empty
			}
			d := saved.Document
			items = append(items, inputPreparationSummary{d.ID, d.Name, len(d.Records), d.UpdatedAt})
		}
		return nil
	})
	if errors.Is(err, errInputPreparationStoreMissing) {
		return items, nil // listing an unused workspace must not create storage
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt == items[j].UpdatedAt {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	return items, nil
}

func (f fileInputPreparationRepository) Get(ctx context.Context, id string) (inputPreparationReply, error) {
	if !inputPreparationID.MatchString(id) {
		return inputPreparationReply{}, errors.New("invalid dialect collection identity")
	}
	view, err := f.Read(ctx)
	if err != nil {
		return inputPreparationReply{}, err
	}
	defer view.Close()
	return view.Get(id)
}

func (f fileInputPreparationRepository) CompareAndSwap(ctx context.Context, doc inputPreparation, revision string) (saved inputPreparationReply, err error) {
	if !inputPreparationID.MatchString(doc.ID) {
		return saved, errors.New("invalid dialect collection identity")
	}
	err = f.locked(ctx, true, func(root *os.Root) error {
		current, e := readInputPreparation(root, doc.ID)
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if e == nil && (revision == "" || current.SHA256 != revision) || errors.Is(e, os.ErrNotExist) && revision != "" {
			return errInputPreparationConflict
		}
		if errors.Is(e, os.ErrNotExist) {
			names, e := inputPreparationNames(root)
			if e != nil {
				return e
			}
			if len(names) >= inputPreparationCountLimit {
				return errInputPreparationCapacity
			}
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		saved, e = writeInputPreparation(root, doc)
		return e
	})
	return
}
