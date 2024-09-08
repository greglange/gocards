# Gocards

Gocards is a flash cards program written in [Go](https://go.dev/) with [spaced repetition](https://en.wikipedia.org/wiki/Spaced_repetition).

Card files and the data files that track the progress of the cards in those files are text files meant to be kept in one or more git repos (or another source control system).

Cards are created using a text editor using [Markdown](https://www.markdownguide.org/basic-syntax/) or some syntax unique to Gocards.

Gocards runs a local web server and cards are practiced in a web browser.

There are few ways images can be used in cards. Cards don't just have to be text.

## Project status

This project is under initial development.

- anything might change
- there are bugs
- no tests
- verified to work on Ubuntu and Windows
- sparse but improving documentation

## Building

Clone or download this project and change dir into the repo.

Then run:

`go mod tidy`

And then:

`go install cmd/gocards.go`

## Quick start usage

Make a directory to hold your cards and card data files.

Change dir into your directory.

Create a file named `esperanto.cd` with this content:

```
book | libro
```

This creates a single card in that file.

Run this command to run a web server to practice your card:

`gocards --http`

Go to this URL in your web browser:

[http://localhost:8080/](http://localhost:8080)

Click on the `esperanto.cd` link.

Click on the `show other side` and `correct` buttons until you see the message `No cards found`at the top of the page.

Click on the `main` button.

Click the `Save` button to save your progress. This writes your progress to a data file.

Note how the card count in the `New` column has changed to zero and the `1` column has a one in it. This means your card has been scheduled to be done again in one day.

While doing spaced repetition, when you get a card right that is beyond the `New` or `0` status, the card will be scheduled to be done again further and further in the future. But, if you get a card wrong, it will return to the `New` or `0` status and you will need to start over building a correct streak with that card.

On the main page, any link that is a gray-shaded cell is spaced repetition practice.

All other links are practice where you need to get each card right once to complete the set. However, this has no effect on the spaced repetition status of the cards.

## Cards repo

A git repo with flash cards usable by the Gocards project can be found [here](https://github.com/greglange/gocards-cards).

The README file in this repo describes how to make card files and cards inside those files.

## Card file location

Card files can be anywhere in the directory tree under the root directory you have selected for your Gocards usage. Just make directories and card files under the root directory and the `gocards` command will find them.

Card files can also be in other locations on your file system. This is useful when using card files that others have made and made available for use (for example in a [git repo](https://github.com/greglange/gocards-cards)) or if you want to make card files you've made available to others.

To use card files that are outside your Gocards root directory, create a `cardFiles` file in your Gocards root directory that tells the `gocards` command where to look for card files.

To specify a directory (and subdirectories) to look for cards in, put a line like this in the file:

```
/home/glange/git/gocards-cards/ spanish
```

(Note the space before `spanish`.)

This will search for card files under `/home/glange/git/gocards-cards/spanish` and write card data files under the directory `spanish` in your Gocards root directory.

To specify a single card file you want to use, put a line like this in the file:

```
/home/glange/git/gocards-cards/ spanish/nouns.cd
```

(Note the space before `spanish`.)

This will let you use the card file `spanish/nouns.cd` found at `/home/glange/git/gocards-cards/spanish/nouns.cd` and the data file for that file will be written inside your Gocards root directory at `spanish/nounds.cdd`.

For now, you need to use full paths in `cardFiles`.

You can use a mixture of local and remote card file locations. The `gocards` command will find card files under your Gocards root directory and any locations in a `cardFiles` file that you create.

It is possible to change where the data files are written inside your Gocards root directory for remote card files by adding a third value to a line in your `cardFiles` file. This is useful if there is a collision between card file names.

For example, look at this `cardFiles` file:

```
/home/glange/git/source1/ spanish spanish1
/home/glange/git/source2/ spanish spanish2
```

This will result in the card files in `/home/glange/git/source1/spanish` being remapped to the directory `spanish1` and the card files in `/home/glange/git/source2/spanish` being remapped to the directory `spanish2` in your Gocards root directory.

To remap the name of a single card file you want to use, do something like this:

```
/home/glange/git/gocards-cards/ spanish/nouns.cd languages/spanish/nouns.cd
```

(Note that you need to specify a card file relative path for the third value.)

Remapping card files and card files path using a `cardFiles` file will result in changing where the data files are written and it will also change the display name for card files when practicing cards in the browser.
