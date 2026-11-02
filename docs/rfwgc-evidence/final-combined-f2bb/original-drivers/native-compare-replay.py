import run as q
import pathlib
s=pathlib.Path(q.M['scratch'])/'tooling'
raise SystemExit(q.run('replay-native-compare',['/home/dev/.local/share/mise/installs/just/1.58.0/just','test','native-compare'],s))
