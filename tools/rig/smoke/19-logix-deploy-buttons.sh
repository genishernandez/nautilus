#!/usr/bin/env bash
# 19 — the Logix deploy buttons (#271) show only where they mean something:
#
#   demoline (target: logix), MainProgram.ld in the TEXT editor: the
#     title bar has "Push Online Edit to Logix Controller", and the "…"
#     menu has "Download Project to Logix Controller"
#   the same file in the DIAGRAM editor: the same two
#   my-plant (`naut new`, no target), interlocks.ld, text and diagram:
#     neither
#
# Visibility only (the nautilus.logixTarget context key and the menus'
# when clauses): what the buttons DO needs logixd and a controller, and was
# verified on Echo by hand (PR #375). Read from the workbench DOM by label;
# the PNGs are evidence.
set -euo pipefail
CHECK=19-logix-deploy-buttons
source "$HOME/smoke/lib.sh"
export G_PACE=fast
source "$HOME/fixtures/gestures.sh"
rm -rf "$PROFILE"

PUSH='Push Online Edit to Logix Controller'
MORE='^More Actions'

# has_push — the title bar shows the Push Online Edit button.
has_push() { [[ $(pg "!!($(title_action_el "$PUSH"))") == true ]]; }
# menu_items — the open context menu's entries, " / "-joined (the menu is
# drawn in a shadow root under the custom title bar).
menu_items() {
  pg '[...document.querySelectorAll(".monaco-menu"), ...[...document.querySelectorAll(".shadow-root-host")].flatMap(h => h.shadowRoot ? [...h.shadowRoot.querySelectorAll(".monaco-menu")] : [])].flatMap(m => [...m.querySelectorAll(".action-label")]).map(a => (a.getAttribute("aria-label") || a.textContent).trim()).filter(Boolean).join(" / ")'
}
# more_menu <tag> — open the title bar's "…" menu, snap it, print its
# entries, close it.
more_menu() {
  local items
  page_click "$(title_action_el "$MORE")" 1.5 || return 0 # no … button: no entries
  items=$(menu_items)
  shot "$1" >/dev/null
  xdotool key --clearmodifiers Escape; sleep 0.5
  echo "$items"
}

one() { # <label> <want: yes|no> <file>
  local what=$1 want=$2 file=$3 png items
  smoke_open "$PROJ" "$file"
  key Escape; hide_sidebar
  sleep 2 # the context key is set after the manifest is read
  for view in text diagram; do
    if [[ $view == diagram ]]; then vs_cmd "nautilus: Open as Diagram Editor" 6; sleep 2; fi
    page_hover "$(title_action_el "$PUSH")" 2>/dev/null || true
    png=$(shot "$what-$view")
    items=$(more_menu "$what-$view-more")
    if [[ $want == yes ]]; then
      has_push && pass "$what $view editor: the Push Online Edit button is in the title bar" "$png" \
        || fail "$what $view editor: no Push Online Edit button" "$png"
      [[ $items == *"Download Project to Logix Controller"* ]] \
        && pass "$what $view editor: the … menu has Download Project to Logix Controller" "$CHECK-$what-$view-more" \
        || fail "$what $view editor: the … menu lacks Download Project ($items)" "$CHECK-$what-$view-more"
    else
      has_push && fail "$what $view editor: a Push Online Edit button on a project with no Logix target" "$png" \
        || pass "$what $view editor: no Push Online Edit button" "$png"
      # Non-empty: the menu opened and was read, so "no Logix entry" is not
      # an unread menu.
      [[ -n $items && $items != *"Logix Controller"* ]] \
        && pass "$what $view editor: no Logix entries in the … menu" "$CHECK-$what-$view-more" \
        || fail "$what $view editor: Logix entries in the … menu ($items)" "$CHECK-$what-$view-more"
    fi
  done
}

ext_fixture demoline
one demoline yes MainProgram.ld
ext_scaffold my-plant
one my-plant no interlocks.ld
