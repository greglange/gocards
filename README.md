# Gocards

Gocards is a flashcards program written in Go with spaced repetition.

## Project status.

This project is under initial development.

- anything might change
- there are bugs
- no tests
- only verfied to work on Ubuntu
- sparse documentation

## Building.

Run:

`go mod tidy`

Then:

`go install cmd/gocards.go`

## Quick start usage.

Make a directory to hold your gocards.

Change dir into your directory.

Create a file named `esperanto.cd` with this content:

```
book | libr/o
```

Run this command to run a web server for your gocards:

`gocards --http`

Go to this url in your web browser to do your gocards:

[http://localhost:8080/](http://localhost:8080)

Click on the `esperanto.cd` link.

Click on the `show other side` and `correct` buttons until you see the message `No cards found`.

Click on the `main` button.

Click the `Update` button to save your progress.

## Cards repo.

A git repo with flashcards usable by the gocards project an be found [here](https://github.com/greglange/gocards-cards).

## Card files.

Card files are text files that end with the file extension:

`.cd`

These files define a set of cards.

Card data files end with the file extension:

`.cdd`

These files track the progress of the cards in the corresponding card file.

## Card ID strings.

Each card has a ID string used in the card data files to track progress.

A single card file cannot contain two or more cards with the same ID string.

## Markdown.

Markdown is used to make cards.

The markdown for each side of the card is converted into HTML when doing a card.

## Single line cards.

A single line card inside a card file looks like this:

`side one | side two`

The ID string for single line cards is the first side of the card.

## Card with a multiline front side.

`This front side

appears on

multiple lines.`

## Version control system.

Gocards is meant to be used with a version control system like `git`.

All gocard files (both the card files and the data files) are plain text.
