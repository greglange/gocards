package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/greglange/gocards/pkg/gocards"

	md "github.com/gomarkdown/markdown"
	mdhtml "github.com/gomarkdown/markdown/html"
	mdparser "github.com/gomarkdown/markdown/parser"

	"golang.org/x/net/html"
)

var mainFuncs = map[string]func(*options) error{
	"http": mainHttp,
}

var boolFlags = []string{}

var stringFlags = []string{"path"}

type options struct {
	b map[string]bool
	s map[string]string
}

func getoptions() *options {
	b := map[string]*bool{}
	s := map[string]*string{}
	for f, _ := range mainFuncs {
		b[f] = flag.Bool(f, false, "")
	}
	for _, f := range boolFlags {
		b[f] = flag.Bool(f, false, "")
	}
	for _, f := range stringFlags {
		s[f] = flag.String(f, "", "")
	}
	flag.Parse()
	o := options{make(map[string]bool), make(map[string]string)}
	for k, v := range b {
		o.b[k] = *v
	}
	for k, v := range s {
		o.s[k] = *v
	}
	if o.s["path"] == "" {
		o.s["path"] = "."
	}
	return &o
}

type cardSetSession struct {
	cardFile         string
	cardSet_         *cardSet
	spacedRepetition bool
	cardType         string
	cardInterval     int
	cardsDone        map[string]bool
	cardSubSet       map[string]bool
	lastCardsSeen    []string
}

type cardSet struct {
	save  bool
	cards []*gocards.Card
}

type httpHandler struct {
	options_ *options
	stats    map[string]*gocards.CardSetStats
	cardSets map[string]*cardSet
	session  *cardSetSession
}

func newHttpHandler(o *options) (*httpHandler, error) {
	stats, err := gocards.GetCardDirectoryStats(o.s["path"])
	if err != nil {
		return nil, err
	}
	return &httpHandler{o, stats, map[string]*cardSet{}, nil}, nil
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// this is supposed to prevent the browser from caching pages
	// https://stackoverflow.com/questions/69597242/golang-prevent-browser-cache-pages-when-clicking-back-button
	w.Header().Set("Cache-Control", "no-cache, private, max-age=0")
	w.Header().Set("Expires", time.Unix(0, 0).Format(http.TimeFormat))
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Accel-Expires", "0")

	if r.URL.Path == "/" {
		r.ParseForm()
		if r.Method == "POST" {
			action := r.FormValue("action")
			if action == "" {
				pageMessage(w, "Action not defined")
			} else if action == "update" {
				err := h.saveCardSets()
				if err != nil {
					pageMessage(w, "Unable to save card sets")
					return
				}
				stats, err := gocards.GetCardDirectoryStats(h.options_.s["path"])
				if err != nil {
					pageMessage(w, "Unable to load card data")
					return
				}
				h.stats = stats
				h.pageMain(w, r)
			} else if action == "main" {
				h.pageMain(w, r)
			} else {
				pageMessage(w, "Inavlid action")
			}
		} else {
			h.pageMain(w, r)
		}
	} else {
		h.cardSet(w, r)
	}
}

func (h *httpHandler) cardSet(w http.ResponseWriter, r *http.Request) {
	var err error
	err = h.populateCardSetSession(r)
	if err != nil {
		pageError(w, err)
	}
	r.ParseForm()
	f, err := h.handleCardSetPost(w, r)
	if err != nil {
		pageError(w, err)
		return
	}
	if f != nil {
		f()
		return
	}
	cards, msg, err := h.getCards()
	if err != nil {
		pageError(w, err)
		return
	}
	if len(cards) == 0 {
		pageMessage(w, "No cards found")
		return
	}
	card, err := h.getCard(cards)
	if err != nil {
		pageError(w, err)
		return
	}
	pageCardFront(w, r.URL.Path, card, msg)
}

func (h *httpHandler) getCard(cards []*gocards.Card) (*gocards.Card, error) {
	if len(cards) == 0 {
		return nil, errors.New("No cards to choose from")
	}
	i := rand.Intn(len(cards))
	card := cards[i]
	if len(cards) > 3 {
		for inSlice(h.session.lastCardsSeen, card.Md5) {
			i = (i + 1) % len(cards)
			card = cards[i]
		}
		h.session.lastCardsSeen = append(h.session.lastCardsSeen, card.Md5)
		if len(h.session.lastCardsSeen) > 3 {
			h.session.lastCardsSeen = h.session.lastCardsSeen[len(h.session.lastCardsSeen)-3:]
		}
	} else {
		h.session.lastCardsSeen = []string{}
	}
	return card, nil
}

func (h *httpHandler) getCards() ([]*gocards.Card, string, error) {
	if h.session == nil {
		return nil, "", errors.New("Session not defined")
	}
	var cards []*gocards.Card
	var msg string
	if h.session.cardType == "all" {
		cards = h.removeCardsDone(h.session.cardSet_.cards)
		msg = "all"
	} else if h.session.cardType == "due_new" {
		cards = gocards.GetDueOrNewCards(h.session.cardSet_.cards)
		msg = "due or new"
	} else if h.session.cardType == "due" {
		cards = gocards.GetDueCards(h.session.cardSet_.cards)
		msg = "due"
	} else if h.session.cardType == "new" {
		cards = gocards.GetIntervalCards(h.session.cardSet_.cards, 0)
		msg = "new"
	} else {
		cards = h.removeCardsDone(gocards.GetIntervalCards(h.session.cardSet_.cards, h.session.cardInterval))
		msg = fmt.Sprintf("interval %d day(s): %d of %d correct", h.session.cardInterval, len(h.session.cardsDone), len(h.session.cardsDone)+len(cards))
	}
	if len(cards) == 0 {
		return cards, msg, nil
	}
	if len(cards) > 10 {
		cardSubSet := make([]*gocards.Card, 0)
		for _, card := range cards {
			_, ok := h.session.cardSubSet[card.Md5]
			if ok {
				cardSubSet = append(cardSubSet, card)
			}

			if len(cardSubSet) >= 10 {
				break
			}
		}
		for len(cardSubSet) < 10 {
			i := rand.Intn(len(cards))
			for {
				card := cards[i]
				_, ok := h.session.cardSubSet[card.Md5]
				if !ok {
					cardSubSet = append(cardSubSet, card)
					break
				}
				i = (i + 1) % len(cards)
			}
		}
		cards = cardSubSet
	}
	for _, card := range cards {
		h.session.cardSubSet[card.Md5] = true
	}
	return cards, msg, nil
}

func (h *httpHandler) handleCardSetPost(w http.ResponseWriter, r *http.Request) (func(), error) {
	if r.Method != "POST" {
		return nil, nil
	}
	action, card, err := h.parseCardSetPost(r)
	if err != nil {
		return nil, err
	}
	if action == "back" {
		f := func() {
			pageCardBack(w, r.URL.Path, card, r.FormValue("msg"))
		}
		return f, nil
	} else if action == "review" {
		review, now := r.FormValue("review"), time.Now()
		if review == "correct" {
			if h.session.spacedRepetition {
				h.session.cardSet_.save = true
				card.LastReviewTime = now
				card.CorrectCount += 1
			} else {
				h.session.cardsDone[card.Md5] = true
			}
		} else if review == "incorrect" {
			if h.session.spacedRepetition {
				h.session.cardSet_.save = true
				card.LastReviewTime = now
				card.CorrectCount = 0
			}
		} else if review == "skip" {
			// fall through
		} else {
			return nil, errors.New("Inavlid review")
		}
	} else if action == "skip" {
		// fall through
	} else {
		return nil, errors.New("Invalid action")
	}
	return nil, nil
}

func (h *httpHandler) pageMain(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "<html><head></head><body>\n")
	fmt.Fprintf(w, "<form action=\"/\" method=\"POST\">\n"+
		"<input type=\"hidden\" name=\"action\" value=\"update\">\n"+
		"<input type=\"submit\" value=\"Update\">\n"+
		"</form>\n")
	fmt.Fprintf(w, "<table border=\"1\">\n")
	fmt.Fprintf(w, "<tr align=\"center\">\n")
	fmt.Fprintf(w, "    <td>Card Set</td>\n")
	fmt.Fprintf(w, "    <td>Total</td>\n")
	fmt.Fprintf(w, "    <td>Blank</td>\n")
	fmt.Fprintf(w, "    <td>New</td>\n")
	fmt.Fprintf(w, "    <td>Due</td>\n")
	intervalValue := -1
	for i := 0; i < len(gocards.Intervals); i++ {
		if intervalValue != gocards.Intervals[i] {
			intervalValue = gocards.Intervals[i]
			fmt.Fprintf(w, "    <td>%d</td>\n", intervalValue)
		}
	}
	fmt.Fprintf(w, "</tr>\n")

	paths := make([]string, 0, len(h.stats))
	for path := range h.stats {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		stats := h.stats[path]
		fmt.Fprintf(w, "<tr align=\"center\">\n")
		fmt.Fprintf(w, "    <td><a href=\"%s\">%s</a></td>\n", path, path)
		fmt.Fprintf(w, "    <td><a href=\"%s/all\">%d</a></td>\n", path, stats.TotalCount)
		fmt.Fprintf(w, "    <td>%d</td>\n", stats.BlankCount)
		fmt.Fprintf(w, "    <td><a href=\"%s/new\">%d</a></td>\n", path, stats.NewCount)
		fmt.Fprintf(w, "    <td><a href=\"%s/due\">%d</a></td>\n", path, stats.DueCount)
		intervalValue := -1
		for i := 0; i < len(gocards.Intervals); i++ {
			if intervalValue != gocards.Intervals[i] {
				intervalValue = gocards.Intervals[i]
				count, ok := stats.IntervalCount[intervalValue]
				if !ok {
					count = 0
				}

				fmt.Fprintf(w, "    <td><a href=\"%s/%d\">%d</a></td>\n", path, intervalValue, count)
			}
		}
		fmt.Fprintf(w, "</tr>\n")
	}
	fmt.Fprintf(w, "</table>\n")
	fmt.Fprintf(w, "</body></html>\n")
}

func pageMessage(w http.ResponseWriter, msg string) {
	fmt.Fprintf(w, "<html><head></head><body>\n")
	fmt.Fprintf(w, "<table><tr><td>\n")
	fmt.Fprintf(w, "<form action=\"/\" method=\"POST\">\n"+
		"<input type=\"hidden\" name=\"action\" value=\"main\">\n"+
		"<input type=\"submit\" value=\"main\">\n"+
		"</form>\n")
	fmt.Fprintf(w, "</td><td><form><label>%s</label></form></td>\n", msg)
	fmt.Fprintf(w, "</tr></table>\n")
	fmt.Fprintf(w, "</body></html>\n")
}

func (h *httpHandler) parseCardSetPost(r *http.Request) (string, *gocards.Card, error) {
	if h.session == nil {
		return "", nil, errors.New("Session not defined")
	}
	action := r.FormValue("action")
	if action == "" {
		return "", nil, errors.New("Action not defined")
	}
	md5 := r.FormValue("md5")
	if md5 == "" {
		return "", nil, errors.New("MD5 not defined")
	}
	var card *gocards.Card
	found := false
	for _, card = range h.session.cardSet_.cards {
		if md5 == card.Md5 {
			found = true
			break
		}
	}
	if !found {
		return "", nil, errors.New("Invalid MD5")
	}
	return action, card, nil
}

func (h *httpHandler) parseCardSetUrl(r *http.Request) (string, bool, string, int, error) {
	var err error
	cardFile, spacedRepetition, cardType, cardInterval := "", false, "", -1
	if strings.HasSuffix(r.URL.Path, ".cd") {
		cardFile = r.URL.Path[1:]
		cardType = "due_new"
		spacedRepetition = true
	} else {
		parts := strings.Split(r.URL.Path[1:], "/")
		if len(parts) < 2 {
			return "", false, "", -1, errors.New("Invalid path")
		}
		if !strings.HasSuffix(parts[len(parts)-2], ".cd") {
			return "", false, "", -1, errors.New("Invalid path")
		}
		cardFile = strings.Join(parts[:len(parts)-1], "/")
		lastPart := parts[len(parts)-1]
		if lastPart == "all" {
			cardType = "all"
		} else if lastPart == "new" {
			cardType = "new"
			spacedRepetition = true
		} else if lastPart == "due" {
			cardType = "due"
			spacedRepetition = true
		} else {
			cardInterval, err = strconv.Atoi(lastPart)
			if err != nil {
				return "", false, "", -1, errors.New("Invalid session interval")
			}
		}
	}
	return cardFile, spacedRepetition, cardType, cardInterval, nil
}

func (h *httpHandler) populateCardSetSession(r *http.Request) error {
	cardFile, spacedRepetition, cardType, cardInterval, err := h.parseCardSetUrl(r)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		cardFile = strings.ReplaceAll(cardFile, "/", "\\")
	}
	_, ok := h.stats[cardFile]
	if !ok {
		return errors.New("Card file not found")
	}
	if r.Method == "GET" {
		cardSet_, ok := h.cardSets[cardFile]
		if !ok {
			cards, err := gocards.LoadCardsAndData(cardFile)
			if err != nil {
				return errors.New("Unable to load card file")
			}
			cardSet_ = &cardSet{false, cards}
			h.cardSets[cardFile] = cardSet_
		}
		h.session = &cardSetSession{cardFile, cardSet_, spacedRepetition, cardType, cardInterval, map[string]bool{}, map[string]bool{}, []string{}}
	}
	return nil
}

func (h *httpHandler) removeCardsDone(cards []*gocards.Card) []*gocards.Card {
	undone := make([]*gocards.Card, 0)
	for _, card := range cards {
		_, ok := h.session.cardsDone[card.Md5]
		if !ok {
			undone = append(undone, card)
		}
	}
	return undone
}

func (h *httpHandler) saveCardSets() error {
	for cardFile, cardSet := range h.cardSets {
		if cardSet.save {
			err := gocards.SaveCardData(cardFile+"d", cardSet.cards, false)
			if err != nil {
				return err
			}
			cardSet.save = false
		}
	}
	return nil
}

func getHtmlPage(requestUrl string) (string, error) {
	resp, err := http.Get(requestUrl)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func image(imageUrl string) string {
	return fmt.Sprintf("<img src=\"%s\">\n", imageUrl)
}

func images(urlString string) string {
	pageUrl, err := url.Parse(urlString)
	if err != nil {
		return fmt.Sprintf("Error parsing url: %s", err)
	}

	data, err := getHtmlPage(urlString)
	if err != nil {
		return fmt.Sprintf("Error getting web page: %s", err)
	}

	imagesString := ""
	tkn := html.NewTokenizer(strings.NewReader(data))
	for {
		tt := tkn.Next()
		if tt == html.ErrorToken {
			break
		}

		image := false
		t := tkn.Token()
		if t.Data == "img" {
			for i, attr := range t.Attr {
				if attr.Key == "alt" {
					t.Attr[i] = html.Attribute{
						attr.Namespace,
						attr.Key,
						"",
					}
				} else if attr.Key == "src" {
					image = true
					url, err := url.Parse(attr.Val)
					if err == nil {
						if url.Host == "" {
							url.Host = pageUrl.Host
						}
						if url.Scheme == "" {
							url.Scheme = pageUrl.Scheme
						}
						t.Attr[i] = html.Attribute{
							attr.Namespace,
							attr.Key,
							url.String(),
						}
					}
				} else if attr.Key == "srcset" {
					t.Attr[i] = html.Attribute{
						attr.Namespace,
						attr.Key,
						"",
					}
				}
			}
			if image {
				imagesString += t.String() + "\n"
			}
		}
	}

	return imagesString
}

func inSlice(s []string, i string) bool {
	for _, j := range s {
		if i == j {
			return true
		}
	}
	return false
}

func markdownToHTML(markdown string) string {
	extensions := mdparser.CommonExtensions | mdparser.AutoHeadingIDs | mdparser.NoEmptyLineBeforeBlock
	p := mdparser.NewWithExtensions(extensions)
	doc := p.Parse([]byte(markdown))

	htmlFlags := mdhtml.CommonFlags | mdhtml.HrefTargetBlank
	opts := mdhtml.RendererOptions{Flags: htmlFlags}
	renderer := mdhtml.NewRenderer(opts)

	return string(md.Render(doc, renderer))
}

func pageCardBack(w http.ResponseWriter, url string, card *gocards.Card, msg string) {
	fmt.Fprintf(w, "<html><head></head><body>\n")
	fmt.Fprintf(w, "<table><tr><td>\n")
	fmt.Fprintf(w, "<form action=\"/\" method=\"POST\">\n"+
		"<input type=\"hidden\" name=\"action\" value=\"main\">\n"+
		"<input type=\"submit\" value=\"main\">\n"+
		"</form>\n")
	fmt.Fprintf(w, "</td><td>\n")
	fmt.Fprintf(w, "<form action=\"%s\" method=\"POST\">\n"+
		"<input type=\"hidden\" name=\"action\" value=\"review\">\n"+
		"<input type=\"hidden\" name=\"md5\" value=\"%s\">\n"+
		"<input type=\"submit\" name=\"review\" value=\"correct\">\n"+
		"<input type=\"submit\" name=\"review\" value=\"incorrect\">\n"+
		"<input type=\"submit\" name=\"review\" value=\"skip\">\n"+
		"</form>\n", url, card.Md5)
	fmt.Fprintf(w, "</td>\n")
	fmt.Fprintf(w, "<td><form><label>%s</label></form></td>\n", msg)
	fmt.Fprintf(w, "</tr></table>\n")
	fmt.Fprintf(w, markdownToHTML(card.Back))
	fmt.Fprintf(w, "</body></html>\n")
}

func pageCardFront(w http.ResponseWriter, url string, card *gocards.Card, msg string) {
	fmt.Fprintf(w, "<html><head></head><body>\n")
	fmt.Fprintf(w, "<table><tr><td>\n")
	fmt.Fprintf(w, "<form action=\"/\" method=\"POST\">\n"+
		"<input type=\"hidden\" name=\"action\" value=\"main\">\n"+
		"<input type=\"submit\" value=\"main\">\n"+
		"</form>\n")
	fmt.Fprintf(w, "</td><td>\n")
	fmt.Fprintf(w, "<form action=\"%s\" method=\"POST\">\n"+
		"<input type=\"hidden\" name=\"action\" value=\"back\">\n"+
		"<input type=\"hidden\" name=\"md5\" value=\"%s\">\n"+
		"<input type=\"hidden\" name=\"msg\" value=\"%s\">\n"+
		"<input type=\"submit\" value=\"show other side\">\n"+
		"<input type=\"submit\" value=\"skip\">\n"+
		"</form>\n", url, card.Md5, msg)
	fmt.Fprintf(w, "</td>\n")
	fmt.Fprintf(w, "<td><form><label>%s</label></form></td>\n", msg)
	fmt.Fprintf(w, "</tr></table>\n")
	if strings.HasPrefix(card.Front, "image:") {
		fmt.Fprint(w, image(card.Front[len("image:"):]))
	} else if strings.HasPrefix(card.Front, "images:") {
		fmt.Fprint(w, images(card.Front[len("images:"):]))
	} else if strings.HasPrefix(card.Front, "wikipedia:") {
		fmt.Fprint(w, wikipediaImages(card.Front[len("wikipedia:"):]))
	} else {
		fmt.Fprint(w, markdownToHTML(card.Front))
	}
	fmt.Fprintf(w, "</body></html>\n")
}

func pageError(w http.ResponseWriter, err error) {
	pageMessage(w, err.Error())
}

func wikipediaImages(searchString string) string {
	requestUrl := fmt.Sprintf("https://en.wikipedia.org/wiki/%s", searchString)
	return images(requestUrl)
}

func main() {
	o := getoptions()
	var err error
	var mainFunc func(*options) error
	for k, v := range mainFuncs {
		if o.b[k] {
			if mainFunc != nil {
				err = errors.New("Only one main option allowed")
			}
			mainFunc = v
		}
	}
	if mainFunc == nil {
		fmt.Println("You must choose a main option")
	} else if err != nil {
		fmt.Println(err)
	} else {
		err = mainFunc(o)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func mainHttp(options *options) error {
	httpHandler, err := newHttpHandler(options)
	if err != nil {
		return err
	}
	http.Handle("/", httpHandler)
	return http.ListenAndServe(":8080", nil)
}
