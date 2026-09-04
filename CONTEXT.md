# Lazysql

Context language for interactive database exploration in terminal UI.

## Language

**Foreign Key Jump**:
Keyboard action that follows a foreign key value from a source row cell to a referenced table view with a prefilled filter.
_Avoid_: FK drilldown, relation follow, link jump

## Relationships

- A **Foreign Key Jump** starts from one selected table cell in a source row.
- A **Foreign Key Jump** targets one referenced table view filtered by the selected value.

## Example dialogue

> **Dev:** "Should Enter open JSON or follow relation here?"
> **Domain expert:** "If this cell supports **Foreign Key Jump**, Enter follows relation; otherwise Enter keeps existing behavior."

## Flagged ambiguities

- "drilldown" was used to mean both JSON inspection and FK navigation - resolved: use **Foreign Key Jump** only for relation navigation.

## Loading state

**Non-blocking Loading**:
A state where a DB operation is in-flight but the UI remains interactive. A status line indicator replaces the previous blocking modal.
_Avoid_: blocking modal, loading overlay

**Loading Indicator**:
A thin text indicator on the table's pagination bar showing "Loading..." while a query runs. Does not steal focus or block input.
_Avoid_: loading spinner modal

**Load Cancellation**:
When a new load is triggered on a table with an in-flight load, the previous operation is cancelled via context cancellation before starting the new one.
_Avoid_: queue, race

**Stale Data**:
Old table results remain visible during a reload. Only cleared on success when new data arrives. Exception: SQL editor queries clear immediately on execute.
_Avoid_: blank screen during load

## Copilot

**Copilot Pane**:
An optional, opt-in chat pane (off by default) shown as a toggleable column next to the results/editor. It talks to GitHub Copilot Chat and is aware of the current connection, provider and schema.
_Avoid_: assistant tab, AI sidebar

**Context Transfer**:
Keyboard-driven, two-way exchange between the SQL editor/results and the Copilot Pane. Editor query, results and schema can be sent into Copilot; a suggested query can be inserted back into the editor.
_Avoid_: copy-paste, auto-sync

**Ask-Before-Execute**:
Copilot never runs SQL on its own. A suggested read-only SELECT can be inserted and run only after an explicit confirmation prompt.
_Avoid_: auto-run, silent execute

**Text-Only Mutation**:
Any modifying SQL (INSERT/UPDATE/DELETE/DROP/…) suggested by Copilot is provided strictly as text to copy and review. There is no run path for it.
_Avoid_: run mutation, execute change

**Row Data Opt-In**:
Sending result-set rows to Copilot is disabled by default and, when enabled, is bounded by a configurable row cap. Schema and column names may be shared without row data.
_Avoid_: send everything, full dump
