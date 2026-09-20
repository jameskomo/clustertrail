# Visual design

The rules the interface follows and the tokens it is built from. Read this
before changing anything visual.

The subject is an instrument panel someone reads under pressure: dense, quiet,
and unambiguous about state. Every choice below follows from that.

## Tokens

Four surfaces, not two. The canvas is darkest, panels sit on it, controls sit
on panels, and dialogs float above. Depth is what makes a dense dashboard
readable. A flat field of hairlines reads as unfinished.

| Role    | Dark      | Light     |
|---------|-----------|-----------|
| Canvas  | `#101319` | `#eef1f6` |
| Panel   | `#171b23` | `#ffffff` |
| Raised  | `#232936` | `#eaeef5` |
| Line    | `#262d3a` | `#e0e5ee` |
| Text    | `#f2f5f9` | `#0f1420` |
| Accent  | `#8b5cf6` | `#4f46e5` |
| Ready / Pending / Failing | `#34d399` / `#fbbf24` / `#fb7185` | `#059669` / `#b45309` / `#e11d48` |

Type is Barlow and JetBrains Mono at a 14px base, with monospace for every
identifier, log line, YAML body and metric. Fonts are self-hosted through
`@fontsource-variable`, so the app makes no runtime request to Google Fonts.

## Principles

- The review dialog is the one bold element. It speaks in a sentence ("You
  are about to scale checkout from 4 to 5 replicas") and its Approve button
  is the only filled button in the product.
- The overview opens with a sentence about the cluster, not a row of tiles.
- Rules and indentation carry structure. No eyebrow labels, no middle-dot
  strings, no decorative gradients.
- Motion only answers an action. The drawer slides in because you opened it.
- Copy is sentence case, active, and names things the way the user does.

## Standing rules

These were settled after several rounds and should not be re-litigated
without a reason.

- **Violet is the accent, and only Approve is filled.** Warm accents read as a
  warning state in this domain. Audit the filled-button rule whenever you add
  a button.
- **The shell is an icon rail, a resource tree, and one topbar.** The rail is
  53px and holds sections, search, theme and engine status. The tree panel is
  208px. The 52px topbar owns every screen's title, description and actions,
  and screens teleport their actions into `#screen-actions` with Vue's
  `<Teleport defer>`, because the target does not exist on first mount.
- **No stat tiles.** Numbers belong in the status sentence or in a table.
- **Icons are hand-authored**, a 16px stroke set in `app/src/icons.ts`. No
  icon library, and no Unicode glyphs standing in for icons.
- **Charts use the violet-anchored ramp** `--chart-a` through `--chart-e`.
- **Filter toggles are `.chip`, either/or pickers are `.seg`.** No 999px
  capsules.
