# Gocards

Gocards is a flash cards program written in [Go](https://go.dev/) with [spaced repetition](https://en.wikipedia.org/wiki/Spaced_repetition).

Card files and the data files that track the progress of the cards in those files are text files meant to be kept in one or more git repos (or other source control systems).

Cards are created using a text editor using [Markdown](https://www.markdownguide.org/basic-syntax/) or some syntax unique to Gocards.

Gocards runs a local web server and cards are done in a web browser.

There are few ways images can be used in cards. They don't just have to be text.

## Project status

This project is under initial development.

- anything might change
- there are bugs
- no tests
- verified to work on Ubuntu and Windows
- sparse but improving documentation

## Building

Run:

`go mod tidy`

Then:

`go install cmd/gocards.go`

## Quick start usage

Make a directory to hold your cards and card data files.

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

Click on the `show other side` and `correct` buttons until you see the message `No cards found`at the top of the page.

Click on the `main` button.

Click the `Save` button to save your progress. This writes your progress to a data file.

Note how the card count in the `New` column has changed to zero and the `1` column has a one in it. This means your card has been scheduled to be done again in one day.

When you get a card right that is beyond the `New` or `0` status while doing spaced repetition, the card will be scheduled to be done again further and further in the future. But, if you get a card wrong, it will return to the `New` or `0` status and you will need to start over building a correct streak with that card.

On the main page, any link that is a gray-shaded cell is spaced repetition practice.

All other links are practice where you need to get each card right once to complete the set. However, this has no effect on the cards spaced repetition status.

## Cards repo

A git repo with flash cards usable by the gocards project can be found [here](https://github.com/greglange/gocards-cards).

The README file in this repo describes how to make card files and cards inside those files.
