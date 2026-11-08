# Finglass design direction: "Kontoauszug, modern"

Approved by the user on 2026-11-08. Finglass first; FinDesk follows in a later, separate change.

## Subject

A German household's books: accounts, bookings, receipts, the tax year. One careful owner who reads numbers. The
app shows where the money stands and what needs doing, with the calm precision of a bank statement.

## Colour

| Token          | Light     | Dark      |
|----------------|-----------|-----------|
| ink            | `#1C2733` | `#E6EAEE` |
| slate          | `#5B6875` | `#9AA5B1` |
| paper          | `#FFFFFF` | `#1A2129` |
| desk           | `#EEF1F4` | `#11161C` |
| rule           | `#DDE2E7` | `#2C3540` |
| petrol         | `#0F6E74` | `#4FB3B9` |
| selected-fill  | `#E4EBEF` | `#232D37` |
| selected-accent| `#DCE6EE` | `#1E3A44` |
| control-fill   | `#E6EAEE` | `#26303A` |
| control-fill-hover | `#E1E6EA` | `#2F3A45` |
| inflow         | `#1E7B4F` | `#5CC28F` |
| alert          | `#B3261E` | `#F2867E` |
| caution        | `#9A5B00` | `#E0A54A` |

### Colour roles

| Role | Token | Use |
|---|---|---|
| Ink | `Theme.text` | content, row titles, outgoing amounts, figures ≥ 0 |
| Slate | `Theme.muted-text` | secondary lines, labels, column headers, inactive icons, chevrons |
| Accent | `Theme.primary` | **only** interactive text (text buttons, links), focus rings, selection bars, the period arrows |
| Inflow | `Theme.money-in` / `success` | incoming money, success states that aren't money |
| Alert | `Theme.error` | negative balances, errors, destructive actions |
| Caution | `Theme.caution` | warnings, "steuerrelevant", open-task counts |

Amount colours: a booking going out is ink with a leading minus; a booking coming in is inflow with "+"; a balance
is ink when ≥ 0 and alert when < 0. Totals of expenses are ink, never red.

## Type

IBM Plex Sans (bundled). Every text follows the role table; use the tokens by role, never a literal size or weight
(the only exception is the bank monogram in `components.slint`).

| Role | Token | Size/weight | Use |
|---|---|---|---|
| Figure | `Type.amount-*` / `Type.headline-*` | 24/600 | KPI figures, the statement result, the period title (Dashboard month, Reports year) |
| Page / dialog title | `Type.title-large-*` | 20/600 | top-bar page title, settings section title, **every dialog title**, empty-state titles |
| Card title | `Type.title-medium-*` | 16/600 | card and panel titles, the bank name on Accounts, document detail title, the compact figures above the Transactions list |
| Navigation | `Type.title-medium-size`; settings `Type.body-large-size` | 16/400 (600 selected); settings 15/500 | the main sidebar items; the settings section list |
| Row title | `Type.label-*` | 14/500 | names in lists, buttons |
| Body | `Type.body-*` | 14/400 | normal text, amounts in rows (amount rows use 14/500, see below) |
| Secondary | `Type.body-small-*` | 13/400 | second lines, notes, help text under fields |
| Small label | `Type.label-small-*` | 13/500 | column headers, KPI labels, group headers, counts next to a title, chips |
| Caption | `Type.caption-*` | 12/500 | chart axis labels and badge counters only |

Row amounts are one choice everywhere: `Type.body-size` with `Type.title-small-weight` (14/500), the majority in
Accounts, Transactions, and the Dashboard's upcoming and recent lists. Amounts right-aligned in table columns.
German amounts are written `2.450,37 €`.

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
