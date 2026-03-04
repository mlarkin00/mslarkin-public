# Newsletter Generator Agent Instructions

This document provides a technical overview of the `template.html` structure. The template has been intentionally annotated with structural `ids`, CSS `classes`, and explicit `data-*` attributes to ensure robust, targeted DOM manipulation via natural language or programmatic scripts.

When you are asked to generate or update the newsletter, ALWAYS reference these structural definitions to guarantee you are targeting or cloning the correct elements.

**CRITICAL RULE**: You must ONLY modify `newsletter.html` when preparing or updating a newsletter. The `template.html` file is strictly for your reference structural definitions and MUST NOT be modified when populating newsletter content.

## 1. Global Structure & Targeting Strategy

The root container resolving the newsletter's max width and aesthetics is:
```html
<div id="newsletter-canvas" class="nl-canvas"> ... </div>
```
Inside this container, the document relies on a series of independent structural blocks prefixed with `nl-section` and defined via explicit `data-component="[COMPONENT_NAME]"`.

**Golden Rule for Section Injection:**
When inserting a new section or modifying an existing one, strictly maintain the `data-component`, `data-element`, and class hierarchies to ensure CSS rules correctly cascade to responsive mobile views.

## 2. Available Components

### 2.1 Header & Announcements
*   **Container**: `div#header[data-component="header"]`
*   **Updatable Elements**:
    *   `img#header-background-image` (Update `src` attribute)
    *   `h2#header-title` (Inner text)
    *   `h2#header-subtitle` (Inner text)
*   **Announcements**: `div#header-announcements[data-component="announcements"]`
    *   `p#banner-text` (Optional top banner message)
    *   `p#introduction-text[data-component="introduction"]` (Main introductory remarks)

### 2.2 Highlights
*   **Container**: `div#highlights[data-component="highlights"]`
*   **List Element**: `ul#highlights-list[data-component="highlights-list"]`
*   **List Items**: Unordered list items using `<li class="highlight-item" data-component="highlight-item">`. Cloned and appended to the `ul` block.

---

### 2.3 Repeated Content Sections

Repeatable content areas for specific subject domains (e.g. "GKE Expert Agents", "Upgrades").
*   **Container**: `div.content-section[data-component="section"][data-section-name="[SECTION_TITLE]"]`
*   **Header Banner**: `div.section-header[data-element="section-header"]`
    *   Title Text: `h2.section-title[data-element="section-title"]`
    *   *Styling Note*: Explicitly set color here via `style="color: rgb({R, G, B});"`. Standard palettes include `rgb(232, 113, 10)` (Orange), `rgb(30, 142, 62)` (Green), or `rgb(26, 115, 232)` (Blue).
*   **Overview**: `div.section-overview[data-element="section-overview"]`
    *   Description Text: `p.section-description[data-element="section-description"]`
    *   Owner Text: `p.section-owner[data-element="section-owner"]`

---

### 2.4 Metrics Scoreboards
Child node rendered inside a valid `<div class="content-section">`.
*   **Container**: `div.metrics-container[data-component="metrics"]`
*   **Item Container**: `div.metric-item[data-component="metric-item"][data-metric-name="[METRIC_TITLE]"]`
*   **Updatable Elements**:
    *   Title: `div.metric-title[data-element="metric-title"]`
    *   Value: `div.metric-value[data-element="metric-value"]`
    *   Sub-Description: `div.metric-description[data-element="metric-description"]`

---

### 2.5 Milestones / OKRs
Child node rendered inside a valid `<div class="content-section">`.
*   **Container**: `div.milestones-container[data-component="milestones"]`
*   **Milestone Group**: `div.milestone-group[data-component="milestone-group"][data-group-name="[GROUP_TITLE]"]` (A single logical grouping of deliverables).
*   **Milestone Item (Row)**: `div.milestone-item[data-component="milestone-item"][data-milestone-name="[MILESTONE_TITLE]"]`
*   **Updatable Row Elements**:
    *   Priority Badge (Col 1): `div.milestone-priority[data-element="priority"]`
    *   Details Column (Col 2): `div.milestone-details[data-element="details"]` (Contains inner `a.milestone-owner[data-element="owner"]` and `span.milestone-target-date[data-element="target-date"]`)
    *   Status Badge (Col 3): `div.milestone-status[data-element="status"]`

---

### 2.6 Contacts
*   **Container**: `div#contacts[data-component="contacts"]`
*   **List Container**: `tbody#contacts-list[data-component="contacts-list"]`
*   **Contact Rows**: `tr.contact-item[data-component="contact-item"][data-contact-role="[ROLE]"]`

---

## 3. Strict Style Guides for Badges

When rendering **Priority** or **Status** badges inside Milestones, **you must apply the exact CSS background and color properties inline** on the inner `.nl-badge` div. Do not rely on external CSS classes for color inheritance.

#### Priority Badges
| Priority | Background (`background:`) | Text Color (`color:`) |
|----------|---------------------------|----------------------|
| **P0**   | `#e72419`                  | `#fff`               |
| **P1**   | `#fa9217`                  | `#fff`               |
| **P2**   | `#fccc00`                  | `#3c4043`            |
| **P?**   | `#e6ffec` (or neutral)     | `#1e8e3e`            |

#### Status Badges
| Status           | Background (`background:`) | Text Color (`color:`) |
|------------------|---------------------------|----------------------|
| **Complete**     | `#e8f0fe`                 | `#1967d2`            |
| **In progress**  | `#e6ffec`                 | `#1e8e3e`            |
| **At risk**      | `#fce8e6`                 | `#c5221f`            |
| **Not started**  | `#f8f9fa`                 | `#3c4043`            |

*Note: Use standard Github Flavored Markdown/Google Alert hex codes for consistency. When inserting these badges, simply replace the inline CSS `background` and `color` properties on the `.nl-badge` element belonging to that row.*

## 4. Workflows

**Creating a new section layout:**
1. Locate the master template block `<!-- REPEATED SECTION BLOCK START -->` inside `template.html`.
2. Extract the parent `<div class="content-section" data-component="section" data-section-name="{{ SECTION_TITLE }}">`.
3. Replace the `{{ }}` variables.
4. If you don't need Metrics or Milestones for this section, simply delete the `<div data-component="metrics">` or `<div data-component="milestones">` child blocks from the extracted string before mounting into `#newsletter-canvas`.
5. Append directly before the `contacts` block.

## 5. Emoji Parsing

When users supply content for the newsletter (like `HIGHLIGHT_ITEM` text or descriptions), they may include an inline emoji placeholder using the format `::NAME::`.

**As the preparing agent, you must parse these placeholders and replace them with the correct HTML-compliant/Unicode emoji character before rendering.**

**Example mapping:**
* `::party::` -> 🎉
* `::warning::` -> ⚠️
* `::check::` -> ✅
* `::rocket::` -> 🚀
* `::bulb::` -> 💡

*(Ensure to resolve any commonly named emoji accurately based on the `NAME` before injecting the rendered HTML).*

### Example Workflow: Adding a Highlight
If a user prompts: *"Add a new highlight: ::rocket:: Launched GKE MCP v2.5"*

1. **Target the File**: Extract and open `newsletter.html` (always ignore `template.html` for edits).
2. **Parse Emoji**: Resolve `::rocket::` into the Unicode character `🚀`.
3. **Locate Insertion Point**: Find the highlights unordered list container, matching the HTML footprint or `data-component="highlights-list"`.
4. **Construct & Append HTML Snippet**:
```html
<li class="highlight-item" data-component="highlight-item" style="margin-bottom: 2px">
  <p style="margin-top: 0; margin-bottom: 0">
    🚀 Launched GKE MCP v2.5
  </p>
</li>
```

## 6. Publishing & Archiving

Once a newsletter is finalized and explicitly approved as "sent", published, or upon user request, **you must copy the contents of `newsletter.html` into `previous.html`**.

**CRITICAL RULE**: `previous.html` must always reflect the exact state of the latest *published* newsletter. This ensures the user has a reliable backup of their most recent send.
