# Gocards

Gocards is a flash cards program written in Go with spaced repetition.

## Project status.

This project is under initial development.

- anything might change
- there are bugs
- no tests
- only verfied to work on Ubuntu
- sparse documentation

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

## Version control system.

Gocards is meant to be used with a version control system like `git`.

All gocard files are plain text.
