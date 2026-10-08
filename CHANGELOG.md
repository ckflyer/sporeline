# Changelog

Each `## version` section becomes the notes on that GitHub Release, so
write it for the people downloading it.

## 1.9

**New**
- **Open folder** buttons on the Data page show your log folder and your
  backups folder in Explorer or Finder.
- Your log can move to another folder: **Change…** opens the computer's own
  folder window (or take a typed path), copies your log, pictures and backups
  there, and uses it from then on. The old folder is left untouched with a
  note saying where everything went. Choosing a folder that already holds a
  Sporeline log switches to that log instead.
- Backups can be kept in a folder of their own, such as another drive or a
  synced folder. Existing backups move across.

**Changed**
- Family trees show more without a click: the whole line back to where it
  started, plus everything from the grandparents down (siblings, cousins,
  children). **Whole tree** shows how many more it would add, and is hidden
  when there is nothing more to see.
- Trees shrink to fit their box instead of running off the right edge. Only a
  very wide family scrolls, starting on the current culture, with faded
  edges to show there is more.
- Each card's colour edge is wider and fills the whole side, round corners
  included, and the outline around the current culture is bolder.
- The three backup boxes on the Data page are now one **Backups** box.
- **Restore…** opens a window like "Choose a parent", listing each backup with
  why it was taken, how many cultures it holds and its size, and asks once
  more before replacing anything.

**Fixed**
- A backup of an empty log (the one taken before a first import) could not
  be restored.
- Two backups taken in the same second overwrote each other.

## 1.8

**Fixed**
- Family trees could put a parent and its children in the same row, with the
  links looping sideways. Generations that had gone wrong are now repaired
  when your log opens: Sporeline IDs get back the generation in their digit,
  and every culture sits below its parents. Generations are only ever raised,
  so deleting a culture's ancestors never changes it.
- The status no longer reads "Gone Gone" when no reason was recorded.
- Search keeps the capitals you typed.
- Recipe scaling understands fractions like 1/2, 1 1/2 and ½, and keeps
  them as fractions.
- Messages after a backup or import no longer garble `+` and `%`.

**Changed**
- Status has its own **Availability** section on each culture. Marking
  something gone now opens a box where you choose what happened, when, and
  an optional note, and you can change those details later.
- The status pill is coloured by reason, so "Used up" no longer looks like a
  failure.
- "Save backup file" includes your pictures, so one file moves everything.
  A smaller version without pictures is also offered.
- Pictures named in your log but missing from disk show as a "Missing" tile,
  and the Data page can put them back from your old pics folder by file name.
- Removing a picture asks first.
- Recipes no longer have the unused category, sterilize and steps fields.
