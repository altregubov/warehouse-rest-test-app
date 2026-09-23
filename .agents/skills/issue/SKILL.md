---
name: issue
description: >-
  Manage, create, update, and close GitHub issues on any repository.
  Use when the user asks to create an issue, list or view issues, update task checkboxes
  based on completed work, attach images/videos to an issue, or close an issue with
  committed and pushed changes.
---

# GitHub Issue Management (`/issue`)

This skill provides step-by-step procedures, templates, and formatting guidelines for managing GitHub issues on any repository using the GitHub CLI (`gh`) or the included `issue_helper.py` script.

## Repository Context & Detection

By default, operations target the current repository automatically:
1. **Auto-Detection**: Inside any git repository with a GitHub remote (`origin`) or configured `gh` default, commands automatically target that repository.
2. **Helper Script Detection**: `issue_helper.py` inspects git remotes, the `gh repo view` context, and standard environment variables (`GH_REPO` / `GITHUB_REPOSITORY`).
3. **Manual Override**: You can target any repository explicitly at any time by passing `-R <owner/repo>` (or `--repo <owner/repo>`) to `gh` or `issue_helper.py`.

---

## 1. Issue Formatting Preferences

When creating issues, always use structured Markdown with task checklists and media placeholders.

### Title Format
Use standard conventional commit style prefixes:
- `feat: <summary>` for new features or capabilities
- `fix: <summary>` for bug fixes
- `docs: <summary>` for documentation
- `refactor: <summary>` for structural refactoring

### Body Format
Always structure the body using the template at [templates/issue_template.md](./templates/issue_template.md):

```markdown
## Overview
<Concise description of the objective, bug, or feature request>

## Tasks
- [ ] Task 1: Initialize implementation
- [ ] Task 2: Implement core functionality
- [ ] Task 3: Verify and write unit tests

## Acceptance Criteria
- [ ] Criteria 1
- [ ] Criteria 2

## Media & Assets
<!-- Screenshots, screen recordings, or diagrams attached using --attach -->
```

---

## 2. Creating an Issue

### Using `gh issue create`
```bash
gh issue create \
  --title "feat: descriptive title" \
  --body-file .agents/skills/issue/templates/issue_template.md
```
*(Optional: add `-R <owner/repo>` if targeting a repository other than current)*

### Creating with Initial Media Attached
```bash
gh issue create \
  --title "feat: add user interface" \
  --body-file issue_body.md \
  --attach "./assets/mockup.png#UI Mockup"
```

### Using Helper Script
```bash
python3 .agents/skills/issue/scripts/issue_helper.py create --title "feat: descriptive title"
```
Or targeting a specific repository:
```bash
python3 .agents/skills/issue/scripts/issue_helper.py -R owner/repo create --title "feat: descriptive title"
```

---

## 3. Updating an Issue Based on Work Done

As tasks are completed during development, keep the issue up to date by checking off tasks and/or posting progress comments.

### Workflow: Check Off Tasks in the Issue Body

1. **View Current State & Tasks**:
   ```bash
   python3 .agents/skills/issue/scripts/issue_helper.py view <issue-number>
   ```
   Or via `gh`:
   ```bash
   gh issue view <issue-number> --json title,body,state
   ```

2. **Toggle Checkbox from `[ ]` to `[x]`**:
   Using the helper script:
   ```bash
   python3 .agents/skills/issue/scripts/issue_helper.py check-task <issue-number> "Task 1"
   # Or by 1-based task number:
   python3 .agents/skills/issue/scripts/issue_helper.py check-task <issue-number> 1
   ```
   Or directly via `gh issue edit`:
   - Retrieve current body: `gh issue view <issue-number> --json body --jq .body > /tmp/body.md`
   - Modify `- [ ] Task Name` to `- [x] Task Name` in `/tmp/body.md`
   - Save back: `gh issue edit <issue-number> --body-file /tmp/body.md`

3. **Add a Progress Comment**:
   When completing a significant milestone or when additional context is needed, post a progress comment using [templates/progress_comment_template.md](./templates/progress_comment_template.md):
   ```bash
   gh issue comment <issue-number> \
     --body "### Work Update\n- [x] Completed task 1\n**Details**: Verified in test suite."
   ```

---

## 4. Attaching Images and Other Media

The GitHub CLI natively supports uploading media files (`.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.mp4`, `.mov`, `.webm` up to 50 files).

### Attachment Syntax
- Flag: `--attach '<path_to_file>#<Alt Text>'`
- Alt text follows the `#` character.
- Alt text is used for images. Videos render in a player without alt text.

### Attaching to an Existing Issue
```bash
gh issue edit <issue-number> \
  --attach "./screenshot.png#Verification Screenshot"
```
Or with the helper:
```bash
python3 .agents/skills/issue/scripts/issue_helper.py attach <issue-number> ./screenshot.png --alt "Verification Screenshot"
```

### Inline Body References
If the markdown body contains a local reference like:
```markdown
![Verification](./screenshot.png)
```
Passing `--attach ./screenshot.png` with `gh issue edit` or `gh issue create` will upload the file and **automatically rewrite** the local path to the hosted GitHub CDN asset URL.

### Attaching to a Comment
```bash
gh issue comment <issue-number> \
  --body "Attached demo video of completed feature:" \
  --attach "./demo.mp4"
```

---

## 5. Closing the Issue with Committed and Pushed Changes

When all tasks and acceptance criteria have been satisfied:

### Step 1: Ensure All Task Checkboxes are Completed
Run:
```bash
python3 .agents/skills/issue/scripts/issue_helper.py view <issue-number>
```
Verify that all tasks show `[DONE]`.

### Step 2: Commit with Issue Linking Keywords
Include GitHub issue closing keywords (`Closes #<num>`, `Fixes #<num>`, or `Resolves #<num>`) in the commit message:
```bash
git commit -m "feat: complete feature implementation

Closes #<issue-number>"
```

### Step 3: Push Changes to Remote
```bash
git push origin <branch-name>
```

### Step 4: Close Issue via CLI
Explicitly ensure the issue is marked closed as completed, linking the commit SHA:
```bash
COMMIT_SHA=$(git rev-parse --short HEAD)
gh issue close <issue-number> \
  --reason "completed" \
  --comment "Resolved in commit ${COMMIT_SHA}. Pushed to origin."
```
Or via helper:
```bash
python3 .agents/skills/issue/scripts/issue_helper.py close <issue-number>
```

### Step 5: Verify Issue Status
```bash
gh issue view <issue-number> --json number,title,state,stateReason
```
Confirm `state` is `"CLOSED"` and `stateReason` is `"COMPLETED"`.

---

## Quick Reference Summary

Task | Command
:--- | :---
**List open issues** | `python3 .agents/skills/issue/scripts/issue_helper.py list`
**View issue & tasks** | `python3 .agents/skills/issue/scripts/issue_helper.py view <num>`
**Create issue** | `python3 .agents/skills/issue/scripts/issue_helper.py create --title "..."`
**Check off task** | `python3 .agents/skills/issue/scripts/issue_helper.py check-task <num> <task-idx-or-name>`
**Attach image/video** | `python3 .agents/skills/issue/scripts/issue_helper.py attach <num> <file> --alt "<alt>"`
**Post comment** | `python3 .agents/skills/issue/scripts/issue_helper.py comment <num> --body "..."`
**Close on commit push**| `git push origin && python3 .agents/skills/issue/scripts/issue_helper.py close <num>`
**Target other repo** | Pass `-R <owner/repo>` to any command or set `GH_REPO=<owner/repo>`

For low-level `gh` command details and JSON output fields, refer to [references/gh_issue_reference.md](./references/gh_issue_reference.md).
