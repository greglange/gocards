package gocards

import (
	"bufio"
	"crypto/md5"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var Intervals = [...]int{0, 0, 0, 1, 1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233, 377}
var IntervalValues = len(Intervals) - 3

type Card struct {
	Md5            string
	Id             string
	InCardFile     bool
	Front          string
	Back           string
	LastReviewTime time.Time
	CorrectCount   int
}

func NewCard(id string, inCardFile bool, front string, back string) *Card {
	md5 := fmt.Sprintf("%x", md5.Sum([]byte(id)))
	return &Card{Md5: md5, Id: id, InCardFile: true, Front: front, Back: back}
}

func NewCardStats(id string, lastReviewTime time.Time, correctCount int) *Card {
	md5 := fmt.Sprintf("%x", md5.Sum([]byte(id)))
	return &Card{Md5: md5, Id: id, InCardFile: false, CorrectCount: correctCount, LastReviewTime: lastReviewTime}
}

func (card *Card) Blank() bool {
	return card.Front == "" || card.Back == ""
}

func (card *Card) Due() (bool, int) {
	now := time.Now()
	interval := card.Interval()
	if interval > 0 {
		elapsed := now.Sub(card.LastReviewTime)
		if elapsed.Hours() < float64(interval)*24 {
			return false, interval
		}
	} else {
		return false, interval
	}

	return true, interval
}

func (card *Card) Interval() int {
	index := card.CorrectCount
	if card.CorrectCount > len(Intervals) {
		index = len(Intervals) - 1
	}
	return Intervals[index]
}

func GetDueCards(cards []*Card) []*Card {
	foundCards := []*Card{}
	for _, card := range cards {
		if card.Blank() {
			continue
		}
		due, _ := card.Due()
		if due {
			foundCards = append(foundCards, card)
		}
	}
	return foundCards
}

func GetDueOrNewCards(cards []*Card) []*Card {
	foundCards := []*Card{}
	for _, card := range cards {
		if card.Blank() {
			continue
		}
		due, interval := card.Due()
		if interval > 0 && !due {
			continue
		}
		foundCards = append(foundCards, card)
	}
	return foundCards
}

func GetIntervalCards(cards []*Card, interval int) []*Card {
	foundCards := []*Card{}
	for _, card := range cards {
		if card.Blank() {
			continue
		}
		if interval == card.Interval() {
			foundCards = append(foundCards, card)
		}
	}
	return foundCards
}

type CardSet struct {
	Id           string
	CardFilePath string
	CardDataPath string
	Cards        []*Card
}

func NewCardSet(id, cardFilePath, cardDataPath string) *CardSet {
	return &CardSet{id, cardFilePath, cardDataPath, nil}
}

func (cs *CardSet) Load() error {
	var err error
	cs.Cards, err = LoadCards(cs.CardFilePath)
	if err != nil {
		return err
	}
	cs.Cards, err = LoadCardData(cs.CardDataPath, cs.Cards)
	if err != nil {
		return err
	}
	return nil
}

func (cs *CardSet) SaveData(clean bool) error {
	return SaveCardData(cs.CardDataPath, cs.Cards, clean)
}

func (cs *CardSet) Stats() *CardSetStats {
	stats := NewCardSetStats(cs.Id)
	for _, card := range cs.Cards {
		stats.TotalCount += 1
		if card.InCardFile {
			stats.CardCount += 1
		} else {
			stats.OldCount += 1
			continue
		}
		if card.Blank() {
			stats.BlankCount += 1
			continue
		}
		due, interval := card.Due()
		_, ok := stats.IntervalCount[interval]
		if ok {
			stats.IntervalCount[interval] += 1
		} else {
			stats.IntervalCount[interval] = 1
		}
		if interval == 0 {
			stats.NewCount += 1
		} else if due {
			stats.DueCount += 1
		}
	}
	return stats
}

type CardSetPath struct {
	RootPath     string
	RelativePath string
	RenamePath   string
}

func LoadCardSetPaths(filePath string) ([]*CardSetPath, error) {
	paths := []*CardSetPath{}
	if _, err := os.Stat(filePath); errors.Is(err, os.ErrNotExist) {
		return paths, nil
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		var path *CardSetPath
		if len(fields) == 2 {
			path = &CardSetPath{fields[0], fields[1], ""}
		} else if len(fields) == 3 {
			path = &CardSetPath{fields[0], fields[1], fields[2]}
		} else {
			// TODO: better error message with line number and number of fields
			return nil, errors.New("Unexpected number of fields")
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func findRootPathCardSets(rootPath string) ([]*CardSet, error) {
	cardSets := []*CardSet{}
	walk := func(path string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if f.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".cd") {
			return nil
		}
		id, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err
		}
		cardSets = append(cardSets, NewCardSet(id, path, path+"d"))
		return nil
	}
	err := filepath.Walk(rootPath, walk)
	if err != nil {
		return nil, err
	}
	return cardSets, nil
}

func findRemotePathCardSets(rootPath string, cardSetPaths []*CardSetPath, cardSets []*CardSet) ([]*CardSet, error) {
	for _, csp := range cardSetPaths {
		walk := func(path string, f os.FileInfo, err error) error {
			var id, dataPath string
			if err != nil {
				return err
			}
			if f.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".cd") {
				return nil
			}
			if strings.HasSuffix(csp.RelativePath, ".cd") {
				if len(csp.RenamePath) > 0 {
					id = strings.TrimSuffix(csp.RenamePath, ".cd")
					dataPath = filepath.Join(rootPath, csp.RenamePath) + ".d"
				} else {
					id = strings.TrimSuffix(csp.RelativePath, ".cd")
					dataPath = filepath.Join(rootPath, csp.RelativePath) + ".d"
				}
			} else {
				relPath, err := filepath.Rel(csp.RootPath, path)
				if err != nil {
					return err
				}
				if len(csp.RenamePath) > 0 {
					renamePath := filepath.Join(csp.RootPath, csp.RelativePath)
					endPath, err := filepath.Rel(renamePath, path)
					if err != nil {
						return err
					}
					id = strings.TrimSuffix(filepath.Join(csp.RenamePath, endPath), ".cd")
					dataPath = filepath.Join(rootPath, csp.RenamePath, endPath) + "d"
				} else {
					id = strings.TrimSuffix(filepath.Join(rootPath, relPath), ".cd")
					dataPath = filepath.Join(rootPath, relPath) + "d"
				}
			}
			if string(os.PathSeparator) != "/" {
				id = strings.ReplaceAll(id, string(os.PathSeparator), "/")
			}
			cardSets = append(cardSets, NewCardSet(id, path, dataPath))
			return nil
		}
		path := filepath.Join(csp.RootPath, csp.RelativePath)
		err := filepath.Walk(path, walk)
		if err != nil {
			return nil, err
		}
	}
	return cardSets, nil
}

func FindCardSets(rootPath string, cardSetPaths []*CardSetPath) ([]*CardSet, error) {
	cardSets, err := findRootPathCardSets(rootPath)
	if err != nil {
		return nil, err
	}
	cardSets, err = findRemotePathCardSets(rootPath, cardSetPaths, cardSets)
	if err != nil {
		return nil, err
	}
	return cardSets, nil
}

func LoadCardSets(cardSets []*CardSet) error {
	for _, cs := range cardSets {
		err := cs.Load()
		if err != nil {
			return err
		}
	}
	return nil
}

type CardSetStats struct {
	Id            string
	IntervalCount map[int]int
	TotalCount    int
	BlankCount    int
	CardCount     int
	DueCount      int
	NewCount      int
	OldCount      int
}

func NewCardSetStats(id string) *CardSetStats {
	return &CardSetStats{Id: id, IntervalCount: make(map[int]int)}
}

func trim(s string) string {
	return strings.Trim(s, " \t")
}

const (
	newCard = iota
	frontMulti
	frontMultiCode
	backMulti
	backMultiCode
)

// returns id, front, parseState
func parseOneSide(s string) (string, string, int) {
	// cases for s:
	// text which is front and id
	// [id]
	// [id] front text
	// [id] `
	// [id] ```

	re := regexp.MustCompile("^\\s*\\[(.+?)\\](.*)$")
	m := re.FindStringSubmatch(s)

	if len(m) == 0 {
		return trim(s), trim(s), newCard
	} else {
		var front string
		var parseState int
		switch trim(m[2]) {
		case "`":
			parseState = frontMulti
		case "```":
			parseState = frontMultiCode
			front = "```"
		default:
			front = trim(m[2])
		}
		return trim(m[1]), front, parseState
	}
}

// returns id, front, back, parseState
func parseTwoSides(s1, s2 string) (string, string, string, int) {
	// cases for s1:
	// text which is front and id
	// [id] text that is the front
	// cases for s2:
	// text which is the back
	// `
	// ```

	re := regexp.MustCompile("^\\s*\\[(.+?)\\](.*)$")
	m := re.FindStringSubmatch(s1)

	var id, front, back string
	var parseState int
	if len(m) == 0 {
		id, front = trim(s1), trim(s1)
	} else {
		id, front = trim(m[1]), trim(m[2])
	}

	switch trim(s2) {
	case "`":
		parseState = backMulti
	case "```":
		parseState = backMultiCode
		back = "```"
	default:
		back = trim(s2)
	}

	return id, front, back, parseState
}

func errorWithLineNumber(err error, lineNumber int) error {
	return errors.New(err.Error() + " on line " + strconv.Itoa(lineNumber))
}

func LoadCards(filePath string) ([]*Card, error) {
	var err error

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fronts := make(map[string]bool)
	cards := make([]*Card, 0, 10)
	addCard := func(id, front, back string) error {
		id = trim(id)
		if len(id) == 0 {
			return errors.New("Id can not be the empty string")
		}
		if _, exists := fronts[id]; exists {
			return errors.New("Duplicate card id")
		}
		fronts[id] = true
		cards = append(cards, NewCard(id, true, trim(front), trim(back)))
		return nil
	}

	var id, front, back string
	var parseState int

	lineNumber := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		lineNumber += 1

		if parseState == newCard {
			if len(line) > 0 && !strings.HasPrefix(line, "#") {
				sides := strings.Split(line, " | ")
				if len(sides) == 1 {
					id, front, parseState = parseOneSide(sides[0])
					err = addCard(id, front, "")
					if err != nil {
						return nil, errorWithLineNumber(err, lineNumber)
					}
				} else if len(sides) == 2 {
					id, front, back, parseState = parseTwoSides(sides[0], sides[1])
					err = addCard(id, front, back)
					if err != nil {
						return nil, errorWithLineNumber(err, lineNumber)
					}
				} else {
					return nil, errorWithLineNumber(errors.New("Unexpected number of sides"), lineNumber)
				}
			}
		} else if parseState == frontMulti {
			if len(cards) == 0 {
				return nil, errorWithLineNumber(errors.New("Unexpected number of card"), lineNumber)
			} else if line == "` | `" {
				parseState = backMulti
			} else if line == "` | ```" {
				parseState = backMultiCode
				cards[len(cards)-1].Back = "```"
			} else if strings.HasPrefix(line, "` | ") {
				parseState = newCard
				back := line[len(" | `"):]
				cards[len(cards)-1].Back = back
			} else if cards[len(cards)-1].Front == "" {
				cards[len(cards)-1].Front = line
			} else {
				cards[len(cards)-1].Front += "\n" + line
			}
		} else if parseState == frontMultiCode {
			if len(cards) == 0 {
				return nil, errorWithLineNumber(errors.New("Unexpected number of card"), lineNumber)
			} else if line == "``` | `" {
				parseState = backMulti
				cards[len(cards)-1].Front += "\n```"
			} else if line == "``` | ```" {
				parseState = backMultiCode
				cards[len(cards)-1].Front += "\n```"
				cards[len(cards)-1].Back = "```"
			} else if strings.HasPrefix(line, "``` | ") {
				parseState = newCard
				back := line[len("``` | "):]
				cards[len(cards)-1].Front += "\n```"
				cards[len(cards)-1].Back = back
			} else {
				cards[len(cards)-1].Front += "\n" + line
			}
		} else if parseState == backMulti {
			if len(cards) == 0 {
				return nil, errorWithLineNumber(errors.New("Unexpected number of card"), lineNumber)
			} else if line == "`" {
				parseState = newCard
			} else if cards[len(cards)-1].Back == "" {
				cards[len(cards)-1].Back = line
			} else {
				cards[len(cards)-1].Back += "\n" + line
			}
		} else if parseState == backMultiCode {
			if len(cards) == 0 {
				return nil, errorWithLineNumber(errors.New("Unexpected number of card"), lineNumber)
			} else if line == "```" {
				parseState = newCard
				cards[len(cards)-1].Back += "\n```"
			} else {
				cards[len(cards)-1].Back += "\n" + line
			}
		} else {
			return nil, errorWithLineNumber(err, lineNumber)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, errorWithLineNumber(err, lineNumber)
	}

	if parseState != newCard {
		return nil, errorWithLineNumber(errors.New("Invalid parse state"), lineNumber)
	}

	return cards, nil
}

// the key for the cards map returned is the file path for each card set
// this means on windows the keys will have \'s
// on linux the keys will have /'s
func LoadCardData(filePath string, cards []*Card) ([]*Card, error) {
	if _, err := os.Stat(filePath); errors.Is(err, os.ErrNotExist) {
		return cards, nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		data := strings.Split(line, " | ")
		if len(data) != 3 {
			return nil, errors.New("Invalid line found in card data")
		}

		id := data[0]
		var lastReviewTime time.Time
		err = lastReviewTime.UnmarshalText([]byte(data[1]))
		if err != nil {
			return nil, err
		}
		correctCount, err := strconv.Atoi(data[2])
		if err != nil {
			return nil, err
		}

		found := false
		for _, card := range cards {
			if card.Id == id {
				found = true
				card.CorrectCount = correctCount
				card.LastReviewTime = lastReviewTime
				break
			}
		}

		if !found {
			cards = append(cards, NewCardStats(id, lastReviewTime, correctCount))
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return cards, nil
}

func LoadCardsAndData(cardsFilepath string) ([]*Card, error) {
	cards, err := LoadCards(cardsFilepath)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Unable to load card set %s: %s", cardsFilepath, err))
	}

	cards, err = LoadCardData(cardsFilepath+"d", cards)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("Unable to load card set data %s: %s", cardsFilepath+"d", err))
	}

	return cards, nil
}

func SaveCardData(filePath string, cards []*Card, clean bool) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, card := range cards {
		if clean && !card.InCardFile {
			continue
		}

		lastReviewTime, err := card.LastReviewTime.MarshalText()
		if err != nil {
			return err
		}

		line := fmt.Sprintf("%s | %s | %d\n", card.Id, lastReviewTime, card.CorrectCount)
		_, err = file.WriteString(line)
		if err != nil {
			return err
		}
	}

	return nil
}
