# The helper (Tsïntsuni, the hummingbird) {#the-helper}

rendimiento has many pages and settings. Its helper answers questions about them: where a setting is, what a page does, why a run failed, how to do something. It answers briefly, with links to where to go. It is a hummingbird: *tsïntsuni* in Purépecha, as in Tzintzuntzan, «the place of hummingbirds», in Michoacán.

## Asking it {#asking-it}

A round button with the hummingbird sits at the bottom right of every page (on a phone, above the bottom menu). It opens a panel beside the page (a sheet from below on a phone). Write the question, or tap one of the quick ones.

- **It knows where you are.** The question goes with the page you asked from. On a run's page it also reads the run: its branch, its status, the steps that did not pass and the diagnostician's explanation, only for an app you may see.
- **It answers with links.** A link to a page of rendimiento opens it there (and closes the sheet on a phone); a link to a page of this book opens in a new tab. It only links to pages that exist: the platform checks each link against the map of its pages (with the apps you may see) and the book's contents, and drops the rest.
- **It says what it read**: the page, and the book's pages that matched the question.
- **It only reads and points.** It changes nothing; asked to do something, it says where you can do it.

## Which AI answers {#which-ai}

The local model (Ollama, set on the [AI page](ai.md)) answers first: it is free and the question stays at home. When it is not convincing, the helper offers **Ask the platform's AI**, which sends the same question to the platform's model, at about what the button says, within the organization's monthly cap. Without a local model it uses the organization's AI.

## What it keeps {#what-it-keeps}

Each person's conversation is theirs alone: nobody else sees it, administrators neither. It is kept 7 days, and what is older is deleted. Agent keys cannot ask it: it is for people signed in.

## In the code {#in-the-code}

| What | Where |
|---|---|
| The helper, its map of pages and how it checks links | `internal/agentes/ayudante.go` |
| `GET /api/ayudante`, `POST /api/ayudante` and what it reads of a run's page | `internal/api/ayudante.go` |
| The conversations, 7 days | `internal/store/ayudante.go`, migration `0070_ayudante.sql` |
| The button and the panel | `web/src/components/ayudante.tsx` |
