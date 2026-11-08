# Finglass design direction: "Kontoauszug, modern"

Approved by the user on 2026-11-08. Finglass first; FinDesk follows in a later, separate change.

## Subject

A German household's books: accounts, bookings, receipts, the tax year. One careful owner who reads numbers. The
app shows where the money stands and what needs doing, with the calm precision of a bank statement.

## Colour tokens

| Token          | Light     | Dark      | Use                                                              |
|----------------|-----------|-----------|------------------------------------------------------------------|
| ink            | `#1C2733` | `#E6EAEE` | text, headings, outgoing amounts                                 |
| slate          | `#5B6875` | `#9AA5B1` | secondary text, labels, inactive icons                           |
| paper          | `#FFFFFF` | `#1A2129` | work surfaces                                                    |
| desk           | `#EEF1F4` | `#11161C` | window background, sidebar                                       |
| rule           | `#DDE2E7` | `#2C3540` | hairlines, outlines                                              |
| petrol         | `#0F6E74` | `#4FB3B9` | the one accent: primary buttons, focus ring, selection bar, links |
| selected-fill  | `#E4EBEF` | `#232D37` | the selected row/nav item background (neutral, not mint)         |
| selected-accent| `#DCE6EE` | `#1E3A44` | the selected Settings section row (petrol tint, no bar)          |
| control-fill   | `#E6EAEE` | `#26303A` | filled buttons, toolbar dropdowns and page-header icon buttons   |
| control-fill-hover | `#E1E6EA` | `#2F3A45` | their hover fill                                              |
| inflow         | `#1E7B4F` | `#5CC28F` | incoming amounts only                                            |
| alert          | `#B3261E` | `#F2867E` | negative balances, errors, destructive confirmation only         |
| caution        | `#9A5B00` | `#E0A54A` | warnings ("tax-relevant", pending items)                         |

Amount colours: a booking going out is ink with a leading minus; a booking coming in is inflow with "+"; a balance
is ink when ≥ 0 and alert when < 0. Totals of expenses are ink, never red.

## Type

IBM Plex Sans (bundled). Page title 20/600 in the top bar. Section titles 15/600, Settings group titles 20/600.
Body 14/400; small body and labels 13/500 in slate, sentence case. Chart axis labels and badge counters are the only
12/500 text. Figures in KPI positions 24/600. Amounts right-aligned in table columns. German amounts are written
`2.450,37 €`.

## Shape and surfaces

One paper surface per page region, divided by hairlines (`rule`), instead of many equal cards. Radius 8 for
surfaces, dialogs, buttons, fields and toolbar controls; no pills for filter controls. No shadows except dialogs and
menus.

Buttons are filled rounded rectangles without an outline: `control-fill` with ink content for quiet and toolbar
actions, `primary` with `on-primary` content for the one primary action. A filled toggle that is on uses the accent.
Icon buttons in page headers and toolbars are filled 36 px squares with a 20 px ink icon; row actions stay
transparent. Toolbar dropdowns use the same fill, height and radius, with the label followed by a small chevron. The
search field is a 36 px paper field with a 1 px `divider` outline, accent while focused. Period navigation is a pair
of 40 px outlined round arrows around a 24/600 title.

Selection normally stays `selected-fill` plus a 3 px petrol bar on the leading edge. Settings section rows are the
exception: an 8 px `selected-accent` tint with ink text and no bar. The main sidebar keeps the phase 1 style
(neutral fill plus bar). Destructive icons in slate; alert only on hover and in the confirmation.

## The signature element

The Dashboard opens with the month's reconciliation set like the foot of a bank statement: one right-aligned column
of figures — Übertrag, + Eingänge, − Ausgänge, ± Umbuchungen — a rule, then Verfügbar. It must add up exactly for the
shown month, in the primary currency only; loans, net worth and other currencies stay outside the column.

## Phases

0. Tokens: move colour and radius literals into `theme.slint` with no visual change (screenshots byte-identical).
1. Palette, selection, amount colours, radii, page title, quiet destructive icons — app-wide.
2. Dashboard reconciliation block and calmer KPI band.
3. Page details: Accounts (no disabled Sync pills while sync is unavailable), Transactions (account panel width,
   select-all placement, "new" dots), Reports (tax list layout, distinct category hues), Settings (rules list).
