# Changelog

Each `## version` section becomes the notes on that GitHub Release, so
write it for the people downloading it.

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
