# Comments (writing to the team) {#comments}

Anyone can write to the team behind rendimiento: an idea, something that did not work, a question. A person on the team reads every comment and answers it in their own words, with their name. The platform's AI only sorts the comments and drafts an answer for that person to rewrite; nothing is ever sent by itself. The design and its reasons are in [Comments (design)](../future/comentarios.md) (decision 145 of la Ruta).

## Writing {#writing}

- **From an installation:** **Write to the team** at the foot of every page (or in the Windows icon's menu) opens `/escribir`. Before you send, **Exactly what will be sent** shows the JSON as it will go: your text, its language, and this installation's version, mode, engine and system. Never your name, your apps or the installation's identity. With it goes the hash of a random token that stays on your computer: only with that token can your installation fetch the answer.
- **Without an account:** the form at `https://rendimiento.ai/comentarios` (linked from the welcome page) asks for your email, only to answer you.
- A comment is up to 2,000 characters. rendimiento.ai takes three an hour from one sender and 200 a day in all.

A comment from an installation goes the way the [señales de vida](senales.md) go, so `SENALES=off`, or turning señales off on that page, turns it off too; the page then points to the public form.

## The answer {#the-answer}

- **In the app:** every three hours an installation asks for the answers it is missing, with each comment's token. When one comes, the bell says **The team answered your comment** until you open `/escribir`, where each of your comments shows its answer.
- **By email:** an answer to the form goes once, from the person who wrote it, with the comment it answers. Nothing else is ever sent to that address.

## What the team sees {#what-the-team-sees}

Where comments are received (`SENALES_RECIBIR=1`, rendimiento.ai), the platform's operators get **Comments** (`/buzon`), with the count in the bell:

- **To review, Answered, Dismissed:** each comment with where it came from and, for the form, the email.
- **What the AI made of it:** its class (*It exists already*, *New*, *Something fails*, *Nonsense*), a one-line summary, the book pages, Ruta entries and earlier comments it matches, and the earlier comment that asks the same thing. The count of a group (*3 people ask this*) is the evidence of demand.
- **The draft,** to rewrite. **Send the answer** asks once more, then sends it with your name. You can also change the class, join it to another comment, **Sort it again**, or **Set aside** (its email is deleted at once).

The AI sees the comment's text only, as data: whatever a comment asks it to do changes nothing. The form's emails are kept apart from the text, seen only on this page, and deleted 30 days after the answer.
