SPORELINE 1.0
A cultivation log that runs on your machine and keeps its data there.

RUNNING IT
  Double-click sporeline.exe. A black console window opens and your
  browser opens to the log. Leave the console window open while you
  work; closing it stops Sporeline.

  Windows will probably warn you that it does not recognise the
  publisher, because the file is not signed. More info -> Run anyway.

WHERE YOUR DATA GOES
  C:\Users\<you>\sporeline\
      sporeline.json   your whole log
      pics\            your pictures, full size

  Nothing is sent anywhere. Back up that folder and you have backed up
  everything. There is also a one-file backup button on the Data page.

IDS
  Six characters: kind, generation, then three random.

      SE = Spores     MY = Mycelium
      SN = Spawn      GR = Grow

  MY2K7P is mycelium, second generation, K7P. Generation 0 is anything
  you did not make yourself. The random characters skip I, O and 0 so
  you can read them off tape later.

COMING FROM MYCOLOG
  1. Close mycolog.
  2. python convert_mycolog.py C:\Users\<you>\mycolog
  3. In Sporeline: Data -> Import -> pick the file it wrote.

  Your old four-character mycolog tokens are kept on every entry and
  searching for them still works, so existing labels stay usable.

OPTIONS
  sporeline.exe -port 9000     use a different port
  sporeline.exe -headless      do not open a browser
  sporeline.exe -data D:\myco  keep the data somewhere else
