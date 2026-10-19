package desk

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

const inputPreparationDir = ".desk-private/input-preparations"
const inputPreparationLimit = 8 << 20

// Private file encoding and custody belong to the file adapter. Feature code
// must use the collection repository, including reads used for admission.
func (s *Server) openInputPreparations(create bool) (*os.Root, error) {
	root, made, err := openOwnFolder(s.root, strings.Split(inputPreparationDir, "/"), create, "input preparations")
	if err == nil && made {
		err = writePrivateData(root, ".gitignore", []byte("*\n"))
		if err != nil {
			root.Close()
		}
	}
	return root, err
}
func readInputPreparation(root *os.Root, id string) (inputPreparationReply, error) {
	raw, err := readPrivateData(root, id+".json", inputPreparationLimit)
	if err != nil {
		return inputPreparationReply{}, err
	}
	return decodeInputPreparation(id, raw)
}

func writeInputPreparation(root *os.Root, doc inputPreparation) (inputPreparationReply, error) {
	if err := validateInputRecordReviews(doc); err != nil {
		return inputPreparationReply{}, err
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return inputPreparationReply{}, err
	}
	if len(raw) > inputPreparationLimit {
		return inputPreparationReply{}, errors.New("input preparation exceeds its storage limit")
	}
	if _, err = decodeInputPreparation(doc.ID, raw); err != nil {
		return inputPreparationReply{}, err
	}
	if err = writePrivateData(root, doc.ID+".json", raw); err != nil {
		return inputPreparationReply{}, err
	}
	return readInputPreparation(root, doc.ID)
}
