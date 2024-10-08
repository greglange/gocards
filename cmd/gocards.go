package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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

// List of main functions, functions that are run because of a command line flag.
var mainFuncs = map[string]func(*options) error{
	"clean": mainClean,
	"http":  mainHttp,
}

var boolFlags = []string{}

var stringFlags = []string{"file", "path"}

type options struct {
	b map[string]bool
	s map[string]string
}

// getOptions parses the command line flags and returns a populated *options.
func getOptions() *options {
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

// Struct to hold information about a session of doing cards.
type cardSetSession struct {
	cardSet          *gocards.CardSet
	spacedRepetition bool
	cardType         string
	cardInterval     int
	cardsDone        map[string]bool
}

// Struct with data needed to serve web pages and respond to requests.
// This struct is passed to the http.Handle function.
type httpHandler struct {
	o        *options
	cardSets []*gocards.CardSet
	session  *cardSetSession
	save     map[string]bool
}

// newHttpHandler returns a populated *httpHandler struct.
// Loads the "cardFiles" file if it exists.
// Finds card set files.
// Loads card files and data files.
// Sorts the cardSets slice by card set id.
// An error is returned if one occurs.
func newHttpHandler(o *options) (*httpHandler, error) {
	cardFilesPath := filepath.Join(o.s["path"], "cardFiles")
	paths, err := gocards.LoadCardSetPaths(cardFilesPath)
	if err != nil {
		return nil, err
	}
	cardSets, err := gocards.FindCardSets(o.s["path"], paths)
	if err != nil {
		return nil, err
	}
	err = gocards.LoadCardSets(cardSets)
	if err != nil {
		return nil, err
	}
	s := func(i, j int) bool {
		return cardSets[i].Id < cardSets[j].Id
	}
	sort.Slice(cardSets, s)
	return &httpHandler{o, cardSets, nil, map[string]bool{}}, nil
}

// ServeHttp serves web pages.
// Parses the path of requests and calls the right function based on that path.
// When a "save" form post is received, any card sets with data that need to be saved are written to disk.
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
			} else if action == "save" {
				err := h.saveCardSets()
				if err != nil {
					pageMessage(w, "Unable to save card sets")
					return
				}
				h.pageMain(w, r)
			} else if action == "main" {
				h.pageMain(w, r)
			} else {
				pageMessage(w, "Invalid action")
			}
		} else {
			h.pageMain(w, r)
		}
	} else {
		h.cardSet(w, r)
	}
}

// cardSet is called by ServeHttp when a request path is for a card set.
// This path is requested by clicking a link on the main page.
// This path is also requested by clicking some of the buttons on a card's page.
// The first request to a card set when doing cards is a GET.
// Populates the card set session in the httpHandler on a GET.
// When doing cards, requests are POSTs.
func (h *httpHandler) cardSet(w http.ResponseWriter, r *http.Request) {
	var err error
	if r.Method == "GET" {
		err = h.populateCardSetSession(r)
		if err != nil {
			pageError(w, err)
			return
		}
	}
	r.ParseForm()
	if r.Method == "POST" {
		f, err := h.handleCardSetPost(w, r)
		if err != nil {
			pageError(w, err)
			return
		}
		if f != nil {
			f()
			return
		}
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

// getCard returns a *gocards.Card from the list of cards passed in.
func (h *httpHandler) getCard(cards []*gocards.Card) (*gocards.Card, error) {
	if len(cards) == 0 {
		return nil, errors.New("No cards to choose from")
	}
	return cards[rand.Intn(len(cards))], nil
}

// getCards returns a list of cards to do based on the values in the handler.
// Also returns a msg string to display at the top of the page.
// Returns an error if one occurs.
// At most 10 cards are returned.
// The list returned is passed to the getCard method to get the card to use.
func (h *httpHandler) getCards() ([]*gocards.Card, string, error) {
	if h.session == nil {
		return nil, "", errors.New("Session not defined")
	}
	var cards []*gocards.Card
	var msg string
	if h.session.cardType == "all" {
		cards = h.removeCardsDone(h.session.cardSet.Cards)
		msg = fmt.Sprintf("all: %d done: %d", len(cards), len(h.session.cardsDone))
	} else if h.session.cardType == "due_new" {
		cards = gocards.GetDueOrNewCards(h.session.cardSet.Cards)
		msg = fmt.Sprintf("due or new: %d done: %d", len(cards), len(h.session.cardsDone))
	} else if h.session.cardType == "due" {
		cards = gocards.GetDueCards(h.session.cardSet.Cards)
		msg = fmt.Sprintf("due: %d done: %d", len(cards), len(h.session.cardsDone))
	} else if h.session.cardType == "new" {
		cards = gocards.GetIntervalCards(h.session.cardSet.Cards, 0)
		msg = fmt.Sprintf("new: %d done: %d", len(cards), len(h.session.cardsDone))
	} else {
		cards = h.removeCardsDone(gocards.GetIntervalCards(h.session.cardSet.Cards, h.session.cardInterval))
		msg = fmt.Sprintf("interval %d day(s): %d done: %d", h.session.cardInterval, len(cards), len(h.session.cardsDone))
	}
	if len(cards) <= 10 {
		return cards, msg, nil
	}
	maxCorrectCount := 0
	for _, card := range cards {
		if card.CorrectCount > maxCorrectCount {
			maxCorrectCount = card.CorrectCount
		}
	}
	correctCount := maxCorrectCount
	cardSubset := []*gocards.Card{}
	for len(cardSubset) < 10 {
		for _, card := range cards {
			if card.CorrectCount == correctCount {
				cardSubset = append(cardSubset, card)
			}
			if len(cardSubset) >= 10 {
				break
			}
		}
		correctCount -= 1
		if correctCount < 0 {
			break
		}
	}
	return cardSubset, msg, nil
}

// handleCardSetPost is called when a POST happens on a card set path.
// Processes "back" button pushes.
// Processes "correct" and "incorrect" button pushes.
// Processes "skip" button pushes.
// For "back" button pushes this retuns a function to call to display the back of the card.
// In all other cases, nil is returned.
// An error is returned if one occurs.
func (h *httpHandler) handleCardSetPost(w http.ResponseWriter, r *http.Request) (func(), error) {
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
				h.save[h.session.cardSet.Id] = true
				card.LastReviewTime = now
				card.CorrectCount += 1
				if card.Interval() > 0 {
					h.session.cardsDone[card.Md5] = true
				}
			} else {
				h.session.cardsDone[card.Md5] = true
			}
		} else if review == "incorrect" {
			if h.session.spacedRepetition {
				h.save[h.session.cardSet.Id] = true
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

// pageMain displays the main page of the web app.
// The URL for this page is just "/".
// The page is a table with rows of card sets and links to do cards.
// The page also has a "save" button that will save data for cards that need to be written to disk.
func (h *httpHandler) pageMain(w http.ResponseWriter, r *http.Request) {
	msg := ""
	if len(h.save) > 0 {
		msg = "needs saving"
	}
	fmt.Fprintf(w, "<html><head></head><body>\n")
	fmt.Fprintf(w, "<table><tr><td>\n")
	fmt.Fprintf(w, "<form action=\"/\" method=\"POST\">\n"+
		"<input type=\"hidden\" name=\"action\" value=\"save\">\n"+
		"<input type=\"submit\" value=\"Save\">\n"+
		"</form>\n")
	fmt.Fprintf(w, "    </td><td>\n")
	fmt.Fprintf(w, "        <form><label>%s</label></form>\n", msg)
	fmt.Fprintf(w, "    </td></tr>\n")
	fmt.Fprintf(w, "</table>\n")
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

	for _, cardSet := range h.cardSets {
		stats := cardSet.Stats()
		fmt.Fprintf(w, "<tr align=\"center\">\n")
		fmt.Fprintf(w, "    <td bgcolor=\"#D3D3D3\"><a href=\"%s\">%s</a></td>\n", stats.Id, stats.Id)
		fmt.Fprintf(w, "    <td><a href=\"%s/all\">%d</a></td>\n", stats.Id, stats.TotalCount)
		fmt.Fprintf(w, "    <td>%d</td>\n", stats.BlankCount)
		fmt.Fprintf(w, "    <td bgcolor=\"#D3D3D3\"><a href=\"%s/new\">%d</a></td>\n", stats.Id, stats.NewCount)
		fmt.Fprintf(w, "    <td bgcolor=\"#D3D3D3\"><a href=\"%s/due\">%d</a></td>\n", stats.Id, stats.DueCount)
		intervalValue := -1
		for i := 0; i < len(gocards.Intervals); i++ {
			if intervalValue != gocards.Intervals[i] {
				intervalValue = gocards.Intervals[i]
				count, ok := stats.IntervalCount[intervalValue]
				if !ok {
					count = 0
				}
				fmt.Fprintf(w, "    <td><a href=\"%s/%d\">%d</a></td>\n", stats.Id, intervalValue, count)
			}
		}
		fmt.Fprintf(w, "</tr>\n")
	}
	fmt.Fprintf(w, "</table>\n")
	fmt.Fprintf(w, "</body></html>\n")
}

// pagemessage displays a webpage with a message on it.
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

// parseCardSetPost parses POST requests to card set urls.
// Returns a string that is the "action" value of the POST.
// Returns the card being done as a *gocards.Card.
// Returns an error if one occurs.
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
	for _, card = range h.session.cardSet.Cards {
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

// isInt returns true of the string is an integer.
func isInt(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

// parseCardSetUrl parses the url for a card set.
// Returns card set id.
// Returns a bool set to true if the URL is for a spaced repetition session.
// Returns string for display that is the type of cards for this session.
// Retruns a non-negative integer if this session is for a particular interval.
// Retuns an error if one occurs.
func (h *httpHandler) parseCardSetUrl(r *http.Request) (string, bool, string, int, error) {
	var err error
	cardSetId, spacedRepetition, cardType, cardInterval := "", false, "", -1
	parts := strings.Split(r.URL.Path[1:], "/")
	if len(parts) < 1 {
		return "", false, "", -1, errors.New("Invalid path")
	}
	lastPart := parts[len(parts)-1]
	if lastPart == "all" {
		cardSetId = strings.Join(parts[:len(parts)-1], "/")
		cardType = "all"
	} else if lastPart == "new" {
		cardSetId = strings.Join(parts[:len(parts)-1], "/")
		cardType = "new"
		spacedRepetition = true
	} else if lastPart == "due" {
		cardSetId = strings.Join(parts[:len(parts)-1], "/")
		cardType = "due"
		spacedRepetition = true
	} else if isInt(lastPart) { // is number
		cardSetId = strings.Join(parts[:len(parts)-1], "/")
		cardInterval, err = strconv.Atoi(lastPart)
		if err != nil {
			return "", false, "", -1, errors.New("Invalid session interval")
		}
	} else {
		cardSetId = strings.Join(parts[:len(parts)], "/")
		cardType = "due_new"
		spacedRepetition = true
	}
	return cardSetId, spacedRepetition, cardType, cardInterval, nil
}

// populateCardSetSession populates the session value in the http handler.
// Session information is determined by parsing the URL.
// Should only be called on the initial GET of session of doing a card set.
// Returns an error if one occurs.
func (h *httpHandler) populateCardSetSession(r *http.Request) error {
	cardSetId, spacedRepetition, cardType, cardInterval, err := h.parseCardSetUrl(r)
	if err != nil {
		return err
	}
	var cardSet *gocards.CardSet
	for _, c := range h.cardSets {
		if cardSetId == c.Id {
			cardSet = c
		}
	}
	if cardSet == nil {
		return errors.New("Invalid card set")
	}
	h.session = &cardSetSession{cardSet, spacedRepetition, cardType, cardInterval, map[string]bool{}}
	return nil
}

// removeCardsDone removes cards from the slice passed in that have been completed in this session.
// This checks the cardsDone variable in the section to determine if a card has been done.
// Returns []*gocards.Cards with cards that have not been done yet.
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

// saveCardSets saves the data for card sets that need to be written to disk.
// returns an error if one occurs.
func (h *httpHandler) saveCardSets() error {
	for cardSetId := range h.save {
		var cardSet *gocards.CardSet
		for _, c := range h.cardSets {
			if cardSetId == c.Id {
				cardSet = c
			}
		}
		if cardSet == nil {
			return errors.New("Unable to find card set")
		}
		dir := filepath.Dir(cardSet.CardDataPath)
		// TODO: what is the right file perms here?
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
		err = cardSet.SaveData(false)
		if err != nil {
			return err
		}
	}
	h.save = map[string]bool{}
	return nil
}

// getHtmlPage gets the web page for the URL passed in.
// Returns the body of the page as a string on success.
// Returns an error if one occurs.
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

// image makes an image html tag from the image url passed in.
// Returns a string that is the tag.
func image(imageUrl string) string {
	return fmt.Sprintf("<img src=\"%s\">\n", imageUrl)
}

// useImage filters images to be displayed.
// An image url is passed in.
// True is returned if the image should be used.
func useImage(imageUrl string) bool {
	if strings.HasPrefix(imageUrl, "https://en.wikipedia.org/static/images/") {
		return false
	} else if strings.HasSuffix(imageUrl, "poweredby_mediawiki.svg") {
		return false
	} else if strings.HasPrefix(imageUrl, "https://upload.wikimedia.org/wikipedia/") {
		if strings.HasSuffix(imageUrl, ".png") {
			return false
		}
		re := regexp.MustCompile("([0-9]+)px")
		m := re.FindStringSubmatch(imageUrl)
		if len(m) > 0 {
			px, err := strconv.Atoi(m[1])
			return err == nil && px >= 100
		}
	}
	return true
}

// images requests the web page for the url passed in and returns a string of image html tags.
// images found on the page are filtered by calling the useImage function.
// Errors are returned as a string if they occur.
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
					url, err := url.Parse(attr.Val)
					if err == nil {
						if url.Host == "" {
							url.Host = pageUrl.Host
						}
						if url.Scheme == "" {
							url.Scheme = pageUrl.Scheme
						}
						imageUrl := url.String()
						t.Attr[i] = html.Attribute{
							attr.Namespace,
							attr.Key,
							imageUrl,
						}
						if useImage(imageUrl) {
							image = true
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

// inSlice returns true if the string is in the slice.
func inSlice(s []string, i string) bool {
	for _, j := range s {
		if i == j {
			return true
		}
	}
	return false
}

// markdownToHTML turns the markdown passed in to html that it returns.
func markdownToHTML(markdown string) string {
	extensions := mdparser.CommonExtensions | mdparser.AutoHeadingIDs | mdparser.NoEmptyLineBeforeBlock
	p := mdparser.NewWithExtensions(extensions)
	doc := p.Parse([]byte(markdown))

	htmlFlags := mdhtml.CommonFlags | mdhtml.HrefTargetBlank
	opts := mdhtml.RendererOptions{Flags: htmlFlags}
	renderer := mdhtml.NewRenderer(opts)

	return string(md.Render(doc, renderer))
}

// cardHtml turns a card side into html.
// The html is written using the http.ResponseWriter.
func cardHtml(w http.ResponseWriter, card string) {
	if strings.HasPrefix(card, "image:") {
		fmt.Fprint(w, image(card[len("image:"):]))
	} else if strings.HasPrefix(card, "images:") {
		fmt.Fprint(w, images(card[len("images:"):]))
	} else if strings.HasPrefix(card, "wikipedia:") {
		fmt.Fprint(w, wikipediaImages(card[len("wikipedia:"):]))
	} else {
		fmt.Fprint(w, markdownToHTML(card))
	}
}

// pageCardBack displays the back of a card.
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
	cardHtml(w, card.Back)
	fmt.Fprintf(w, "</body></html>\n")
}

// pageCardFront displays the front of a card.
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
	cardHtml(w, card.Front)
	fmt.Fprintf(w, "</body></html>\n")
}

// pageError displays the error.
func pageError(w http.ResponseWriter, err error) {
	pageMessage(w, err.Error())
}

// wikipediaImages gets the images on a wikipedia page.
func wikipediaImages(searchString string) string {
	requestUrl := fmt.Sprintf("https://en.wikipedia.org/wiki/%s", searchString)
	return images(requestUrl)
}

// main parses the command line options and calls the right main function.
func main() {
	o := getOptions()
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

// mainClean removes cards from the data file that no longer exist in the cards file.
// TODO: fix this - this broke with the remote card set change
func mainClean(o *options) error {
	if o.s["file"] == "" {
		return errors.New("--file must be specified")
	}
	filePath := filepath.Join(o.s["path"], o.s["file"])
	cards, err := gocards.LoadCardsAndData(filePath)
	if err != nil {
		return err
	}
	err = gocards.SaveCardData(filePath+"d", cards, true)
	if err != nil {
		return err
	}
	return nil
}

// mainHttp serves webpages.
func mainHttp(o *options) error {
	httpHandler, err := newHttpHandler(o)
	if err != nil {
		return err
	}
	http.Handle("/", httpHandler)
	return http.ListenAndServe(":8080", nil)
}
