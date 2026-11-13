# Legal, Interoperability, & Regulatory Notice (`TinkeringDao/techo5`)

`TinkeringDao/techo5` is an independent, non-commercial fork of the MIT-licensed [TECHO5](https://github.com/HuskerMinion/techo5) project, maintained under [`TinkeringDao`](https://github.com/TinkeringDao). It follows the account-wide 8-pillar policy of `TinkeringDao`, applied to the hardware and software scope below. The upstream MIT [`LICENSE`](./LICENSE) (preserving upstream copyright attribution alongside `TinkeringDao` fork additions) and [`NOTICE`](./NOTICE) govern all upstream and derived code.

---

## 1. Independent Non-Commercial Open-Source Project, Indication of Intended Purpose, & Nominative Fair Use

This repository is an **independent, non-commercial, community-developed free and open-source project** that enables hardware interoperability and open-source firmware customization on lawfully acquired consumer hardware owned by the operator.

- **No Affiliation or Endorsement:** This project is **not affiliated with, authorized by, sponsored by, or endorsed by** Amazon.com, Inc. or its affiliates, MediaTek Inc., or any other referenced third-party hardware or semiconductor manufacturer.
- **Indication of Intended Purpose & Nominative Fair Use:**
  - Pursuant to **Article 14(1)(c) of Regulation (EU) 2017/1001 (EUTMR)**, settled CJEU jurisprudence (*Case C-228/03 — Gillette v. LA-Laboratories*; *Case C-63/97 — BMW v. Deenik*), and **US Nominative Fair Use (`15 U.S.C. §§ 1115(b)(4), 1125`**, *New Kids on the Block v. News America Publishing, Inc.*, 971 F.2d 302 (9th Cir. 1992)), the names "Amazon Echo Show 5 (2nd generation)" and "MediaTek MT8163" are used strictly as a necessary indication of intended purpose and target hardware compatibility of the software.
  - No corporate logos, proprietary icons, vendor application screenshots, or proprietary trade dress of any third party are distributed by this fork.

---

## 2. Statutory Software/Hardware Interoperability & Trade Secret Safe Harbor

Interface knowledge used by this project (register layouts, data formats, wire protocols of the target device) is obtained via clean-room observation, published datasheets, or licensed open-source upstream trees on lawfully acquired hardware owned by the operator.

### 2.1 European Union (`Directive 2009/24/EC` & `Directive (EU) 2016/943`)
1. **Exclusion of Functional Interfaces from Copyright (`Article 1(2) of Directive 2009/24/EC`):** Ideas, principles, functional interfaces, data formats, and register layouts are excluded from copyright protection (*SAS Institute Inc. v. World Programming Ltd*, Case `C-406/10`, paras. 39–46).
2. **Right to Observe, Study, and Test (`Article 5(3) of Directive 2009/24/EC`):** The lawful user of a device may observe, study, and test its functioning to determine the underlying ideas and principles.
3. **Right of Interoperability (`Article 6 of Directive 2009/24/EC`):** Independent analysis necessary to achieve the interoperability of an independently created program with existing hardware and software is permitted where interface information was not otherwise available.
4. **Nullity of Contrary Contract Terms (`Article 8 of Directive 2009/24/EC`):** EULA clauses restricting rights under `Article 5(3)` or `Article 6` are null and void.
5. **Lawful Reverse Engineering (`Article 3(1)(b) of Directive (EU) 2016/943`):** Observing, studying, disassembling, or testing a product lawfully in the possession of the observer is a lawful acquisition of information.

### 2.2 United States (`17 U.S.C. §§ 102(b), 107, 1201(f)` & `18 U.S.C. § 1839(6)(B)`)
1. **Uncopyrightable Methods of Operation (`17 U.S.C. § 102(b)`):** Register offsets, command IDs, and functional facts necessary for hardware compatibility are not copyrightable (*Baker v. Selden*, 101 U.S. 99 (1879); *Google LLC v. Oracle America, Inc.*, 593 U.S. 1 (2021)).
2. **Interoperability Exemptions (`17 U.S.C. § 1201(f)`, `§ 107`):** Developed exclusively to achieve interoperability of independently created programs with lawfully owned consumer hardware (*Sega Enterprises Ltd. v. Accolade, Inc.*, 977 F.2d 1510 (9th Cir. 1992)).
3. **DTSA Safe Harbor (`18 U.S.C. § 1839(6)(B)`):** Reverse engineering and independent derivation of a lawfully acquired product are not "improper means".

---

## 3. Non-Circumvention of Technological Protection Measures

This repository does not circumvent, bypass, decrypt, or defeat any Technological Protection Measure.

- **No Key Extraction or Secure-Boot Bypass:** The repository contains no proprietary cryptographic keys, OEM private keys, seeds, certificates, or secure-boot exploits, and never decrypts or patches encrypted OEM firmware partitions.
- **Own Release Key:** Update manifests are verified against this fork's own Ed25519 public key. The corresponding private seed is held outside the repository.

---

## 4. Zero Proprietary Code, Verbatim Firmware Bytes, or Binary Distribution

- **Original or Properly Licensed Code:** Source code is original clean-room code or properly attributed open-source code distributed under its respective license (upstream `MIT`, with the attributions in [`NOTICE`](./NOTICE)).
- **No OEM Binaries or Assets:** The repository does not host, redistribute, or link to proprietary OEM firmware images, vendor application packages, decompiled vendor source code, or verbatim proprietary firmware byte slices.

---

## 5. Authorized Local Owner Execution Only (`18 U.S.C. § 1030` — CFAA)

In accordance with the **Computer Fraud and Abuse Act (`18 U.S.C. § 1030`)** (*Van Buren v. United States*, 593 U.S. 374 (2021)):

- **No Access-Security Circumvention:** The software neither bypasses authentication barriers nor intercepts non-public transmissions.
- **Local Execution by the Physical Owner:** All utilities and firmware builds are designed solely for execution by the physical owner on their own lawfully acquired hardware. This build opens outbound connections only to RFC 1918 private LAN, RFC 6598 overlay-mesh, and IPv6 ULA destinations; manufacturer cloud servers and public endpoints are refused by `netguard.LocalOnlyDialer`. Executing this software against third-party devices without the owner's explicit authorization is prohibited.

---

## 6. Non-Commercial Open-Source Software Exemption (EU CRA `Regulation (EU) 2024/2847` & EU PLD `Directive (EU) 2024/2853`)

- **EU Cyber Resilience Act (`Regulation (EU) 2024/2847`, Recitals 15 & 18; `Art. 2(5)`; `Art. 3(13)-(14)`):** This repository is developed, maintained, and supplied strictly as **non-commercial free and open-source software outside the course of any commercial activity**. No fee is charged, no paid support is offered, and no personal data is collected or monetized. The authors are therefore neither "manufacturers" (`Art. 3(13)`) nor "open-source software stewards" (`Art. 3(14)`).
- **EU Product Liability Directive (`Directive (EU) 2024/2853`, `Art. 2(2)` & Recitals 14, 15, 24):** Strict product liability does not apply to free and open-source software developed or supplied outside the course of a commercial activity. Any third party who integrates or redistributes this software commercially does so under its own responsibility as a manufacturer.

---

## 7. Radio Frequency (RF) & Spectrum Regulatory Compliance (`Directive 2014/53/EU (RED)`, `ETSI EN 300 328`, `FCC 47 C.F.R. Part 15`)

The software uses the target device's existing Wi-Fi and Bluetooth radios through the device's standard drivers and never alters certified baseband RF calibration tables, carrier frequencies, or radiated power limits. The operator is responsible for compliance with the radio regulations of the jurisdiction in which the device is operated.

---

## 8. AS IS Warranty Disclaimer & Hardware Risk Notice

ALL SOFTWARE, FIRMWARE SOURCE CODE, AND DOCUMENTATION IN THIS REPOSITORY ARE PROVIDED **"AS IS"**, WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES, OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT, OR OTHERWISE, ARISING FROM, OUT OF, OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

- **Operator's Sole Risk:** Compiling, flashing, or executing firmware is performed entirely at the operator's own risk. The authors and contributors assume no liability for hardware malfunction, bricked devices, battery or thermal damage, regulatory non-compliance by the operator, or warranty disputes.

---

## Addendum: German Statutory References (Applicable to Residents of Germany)

Where German national law applies alongside European Union regulations and directives, the following national provisions govern this repository:

- **Trademark Indication of Intended Purpose (`§ 23 Abs. 1 Nr. 3 MarkenG` — *Bestimmungsangabe*):** Section 23(1) No. 3 of the German Trademark Act (implementing `Art. 14(1)(c) EUTMR`; BGH `I ZR 6/10`) permits referencing third-party product names and chip designations where necessary to indicate the intended purpose and target hardware compatibility of independent software.
- **Software Interoperability & Uncopyrightable Interfaces (`§§ 69a Abs. 2 & 5, 69d Abs. 3, 69e, 69f Abs. 2, 69g Abs. 2, 95a UrhG`):** Under the German Copyright Act (implementing `Directive 2009/24/EC`):
  - `§ 69a Abs. 2 Satz 2 UrhG` excludes ideas, principles, and functional interfaces underlying a computer program from copyright protection.
  - `§ 69d Abs. 3 UrhG` entitles the lawful user to observe, study, or test the functioning of a program to determine its underlying ideas and principles.
  - `§ 69e UrhG` authorizes independent analysis necessary to achieve the interoperability of an independently created computer program (the counterpart of `Art. 6 of Directive 2009/24/EC`).
  - `§ 69g Abs. 2 UrhG` renders contractual clauses contrary to `§ 69d Abs. 2, 3` or `§ 69e UrhG` null and void (*nichtig*).
  - `§ 69a Abs. 5 UrhG` excludes computer programs from the general technological protection measure rules (`§ 95a UrhG`); this project circumvents no technical protection mechanism (`§ 69f Abs. 2 UrhG`).
- **Reverse Engineering of Lawfully Acquired Hardware (`§ 3 Abs. 1 Nr. 2 lit. a & b GeschGehG`):** The German Trade Secrets Act (implementing `Article 3(1)(b) of Directive (EU) 2016/943`) authorizes obtaining information by observing, examining, disassembling, or testing a product that has been made publicly available (`lit. a`) or is in the lawful possession of the observer (`lit. b`).
- **German IT Criminal Law & Property Ownership (`§§ 202a–202c, 303a StGB`, `§ 903 BGB`):** No access security mechanism (*Zugangssicherung*) is bypassed (`§§ 202a, 202c StGB`), no non-public transmission is intercepted (`§ 202b StGB`), and all builds operate exclusively on the physical owner's (`§ 903 BGB`) own device data (*eigene Daten*, `§ 303a StGB`).
- **Radio Equipment (`FuAG`, implementing `Directive 2014/53/EU (RED)`):** The operator is solely responsible for operating the device's radios within the applicable rules of the German Radio Equipment Act.
- **Gratuitous Open-Source Liability Standard (`§§ 521, 523, 524, 599, 600 BGB`):** Because the software and documentation are provided gratuitously (*unentgeltlich*) outside the course of commercial activity, statutory liability under the German Civil Code is limited to intent (*Vorsatz*) and gross negligence (*grobe Fahrlässigkeit*).
