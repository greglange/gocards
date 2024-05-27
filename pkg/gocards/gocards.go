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

type CardSetStats struct {
	IntervalCount map[int]int
	TotalCount    int
	BlankCount    int
	CardCount     int
	DueCount      int
	NewCount      int
	OldCount      int
}

func NewCardSetStats() *CardSetStats {
	return &CardSetStats{IntervalCount: make(map[int]int)}
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
		switch trim(m[1]) {
		case "`":
			parseState = frontMulti
		case "```":
			parseState = frontMultiCode
		default:
			front = trim(m[1])
		}
		return trim(m[0]), front, parseState
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

func GetCardFileStats(cardsFilepath string) (*CardSetStats, error) {
	cards, err := LoadCardsAndData(cardsFilepath)
	if err != nil {
		return nil, err
	}

	stats := NewCardSetStats()

	for _, card := range cards {
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

	return stats, nil
}

func GetCardDirectoryStats(directoryPath string) (map[string]*CardSetStats, error) {
	stats := make(map[string]*CardSetStats)
	walk := func(path string, f os.FileInfo, err error) error {
		if f.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".cd") {
			return nil
		}
		stats[path], err = GetCardFileStats(path)
		if err != nil {
			return err
		}
		return nil
	}

	err := filepath.Walk(directoryPath, walk)
	if err != nil {
		return nil, err
	}
	return stats, nil
}
