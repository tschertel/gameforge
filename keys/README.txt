Put here the prod.keys dumped from your own console (e.g. with Lockpick_RCM).

GameForge looks for keys/prod.keys automatically, in the folder of the
executable and in the folder it was started from.

To convert Wii discs to RVZ (GameCube/Wii -> RVZ tab), also put the Wii
common key here, as wii_common_key.bin (16 bytes) or wii_common_key.txt
(32 hexadecimal digits). Korean discs use the Korean key:
wii_korean_key.bin. GameForge contains no keys.

To extract PS3, PSP and PS Vita packages (PKG tab), put your own PKG
keys in pkg.keys, one per line, as name = 32 hexadecimal digits:
ps3, psp, vita2, vita3, vita4. For PS4 fake packages built with a
passcode other than all zeros, also the two PS4 RSA keysets, each as
modulus and private exponent (512 hexadecimal digits): ps4_fake_n,
ps4_fake_d, ps4_dk3_n, ps4_dk3_d. Other PS4 packages and debug packages
need no keys.

These files are personal: do not share or publish the keys.
