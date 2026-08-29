# tsp interface system

## Direction

`tsp dash` is a keyboard-first control surface for Matt while he is already
working inside tmux. It should feel dense, calm, immediate, and native to the
terminal. The task being entered is always the focal point. Provider, model,
path, branch, command, and workspace context support it without competing.

The signature interaction is an inline `‹ CLAUDE ›` / `‹ CODEX ›` provider
selector beside an editable run-specific setting and a visible command
preview. Use the same pattern in Spawn and Delegate forms.

## Product world

- Sessions, panes, processes, worktrees, branches, agents, and scrollback.
- Charcoal terminal canvas, pale terminal ink, mint activity, amber idle state,
  quiet graphite rules, and muted process metadata.
- Reject generic card grids, mouse-first controls, decorative color, modal
  animations, and controls that hide the command being launched.

## Palette

Use only the dashboard tokens in `internal/cmd/dash.go`:

| Token | Value | Role |
|---|---|---|
| `dashCanvas` | `#111513` | Terminal canvas and header surface. |
| `dashSelected` | `#1C2923` | Selected roster row. |
| `dashRule` | `#2A322E` | Dividers and modal borders. |
| `dashFaint` | `#59615D` | Section labels and inactive structure. |
| `dashMuted` | `#8A948F` | Supporting text and metadata. |
| `dashInk` | `#E7EBE8` | Primary values and titles. |
| `dashMint` | `#9FE8C3` | Running state, focused keys, and active selectors. |
| `dashAmber` | `#E5B566` | Idle or exited state. |
| `dashDanger` | `#E38B84` | Destructive confirmation only. |

Color communicates state or focus. Do not add decorative hues.

## Depth and spacing

- Depth strategy: borders only. Do not add shadows or simulated elevation.
- Base unit: one terminal row vertically and two terminal columns horizontally.
- Modal frame: normal one-cell border, `dashRule`, one row and two columns of
  inner padding, one row and two columns of outer margin.
- Group a label tightly with its value. Separate distinct fields with one blank
  row. Keep command and workspace metadata together below editable fields.
- Repeated keyboard actions have no animation.

## Hierarchy

- Terminal typography stays native. Build hierarchy with weight, color, case,
  and spacing rather than font size.
- Primary: bold `dashInk` for titles and values.
- Secondary: `dashMuted` for supporting text.
- Tertiary: bold uppercase `dashFaint` for section and field labels.
- Focus/action: bold `dashMint`. Destructive titles alone use `dashDanger`.
- The task input is first and focused when a form opens.
- Wide dashboard breakpoint: 86 columns. Above it, roster and detail sit side
  by side with the roster at 36% width. Below it, use a compact one-line roster
  above the detail pane.
- Every view must be clipped to the current terminal width and height.

## Component patterns

### Dashboard roster

- Wide rows use three lines: provider/state, task or session title, then branch
  or pane metadata.
- Narrow rows use one line: status glyph, provider, then title.
- Status glyphs are `●` running, `◌` present but idle/exited, and `○` missing.
- Delegated runs sit directly below their parent with two-column indentation.
- The selected row uses `dashSelected`; unselected rows stay on the canvas.

### Spawn form

- Field order: Task, Project Path, Agent, Base Branch.
- Provider selector uses `‹ PROVIDER ›`; left, right, or space toggles it only
  while that field is focused.
- Base Branch is editable and starts with the detected current branch when the
  project is a git repository. Empty uses the current branch.
- Show the exact configured provider command below the fields.

### Delegate form

- Field order: Task, Agent, Model.
- Provider changes load that provider's configured default model. The model
  remains editable for the current run.
- Keep workspace and parent context visible below the fields.
- Show a shell-safe command preview with `<delegation prompt>` in place of the
  generated prompt.
- A delegated tmux pane is temporary. Close it when the agent exits and retain
  its final bounded scrollback in the dashboard run record.

### Controls and states

- `Tab` and `Shift+Tab` move between fields. Arrow keys act only on provider
  selectors. `Enter` submits. `Esc` cancels.
- Footers list the available keys in interaction order.
- Busy actions replace the dashboard footer with `◌` and a plain status line.
- Empty, running, idle/exited, missing, error, confirmation, and success states
  must remain visible and bounded at narrow terminal sizes.

## Verification

- Keep unit coverage for focus order, provider changes, configured commands,
  branch/model values, and terminal bounds.
- Smoke-test modal rendering at about 120×42 and the compact dashboard at or
  below 72 columns.
- In monochrome, title, selected row, active status, task field, and footer
  should still form a clear order without relying on hue.
