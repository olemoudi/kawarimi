// Package copytext holds the recipient-facing instructions, in one place, so the
// package README, the release email, and the in-vault docs never drift apart. Text
// is bilingual: Spanish first (the intended recipients), then English.
package copytext

import (
	"fmt"
	"strings"
)

// binaryLabel maps a cross-compiled binary name to a human OS label (ES / EN).
func binaryLabel(name string) string {
	switch {
	case strings.Contains(name, "windows"):
		return "Windows"
	case strings.Contains(name, "darwin-arm64"):
		return "Mac (Apple Silicon: M1/M2/M3)"
	case strings.Contains(name, "darwin-amd64"):
		return "Mac (Intel)"
	case strings.Contains(name, "linux-arm64"):
		return "Linux (ARM)"
	case strings.Contains(name, "linux-amd64"):
		return "Linux (PC)"
	default:
		return name
	}
}

func binaryList(binaries []string) string {
	if len(binaries) == 0 {
		return "  (no program was bundled — ask a technical person to build kawarimi for you)\n"
	}
	var b strings.Builder
	for _, name := range binaries {
		fmt.Fprintf(&b, "  - %-28s %s\n", name, binaryLabel(name))
	}
	return b.String()
}

// PackageInstructions returns the INSTRUCTIONS.md shipped inside the package. The
// recipient already has the files; they still need the key (email) and the card.
func PackageInstructions(binaries []string, buildDate string) string {
	return fmt.Sprintf(`# Cómo abrir esta caja fuerte / How to open this vault

Package built / Paquete creado: %s

Programs included for each computer / Programas incluidos para cada ordenador:
%s
====================================================================
ESPAÑOL
====================================================================

Necesitas DOS cosas para abrir esta caja fuerte:

  1. La CLAVE que te llegó por correo electrónico.
  2. Las PALABRAS escritas en la tarjeta física que te dio el titular.

Ninguna de las dos por separado sirve: hacen falta las dos.

PASOS

  1. Ya tienes los archivos (están en esta carpeta).
  2. Abre el programa kawarimi para TU ordenador — búscalo en la lista de
     arriba ("Programas incluidos"):
       - Windows: haz doble clic en el programa que termina en .exe
       - Mac:     hay dos programas para Mac: "Apple Silicon" para Macs de 2021
                  en adelante (M1/M2/M3...) e "Intel" para Macs más antiguos;
                  si no lo sabes, prueba primero el de Apple Silicon.
                  Abre "Terminal", arrastra el programa a la ventana,
                  escribe un espacio y la palabra  open  y pulsa Intro
       - Linux:   igual que en Mac: abre un terminal, arrastra el programa,
                  escribe un espacio y la palabra  open  y pulsa Intro
  3. El programa te preguntará:
       - Pega la CLAVE del correo electrónico.
       - Escribe las PALABRAS de la tarjeta.
  4. Tus archivos aparecerán en la carpeta "decrypted".
     Abre primero el archivo INDEX.md: es el índice de todo.

SI EL SISTEMA TE AVISA DE QUE EL PROGRAMA "NO ES SEGURO"

  Es normal: el programa no está firmado, pero es el que te dejó el titular.
   - Windows (SmartScreen): pulsa "Más información" y luego "Ejecutar de todas formas".
   - Mac: al bloquearlo, abre Ajustes del Sistema > Privacidad y seguridad,
     baja hasta el aviso sobre el programa, pulsa "Abrir de todas formas"
     y vuelve a ejecutarlo. (En Macs antiguos también funciona: clic derecho
     en el programa > "Abrir" > confirmar.)

SI EL PROGRAMA SE CIERRA SOLO O NO LLEGA A ABRIR

  Necesita un ordenador con al menos 2 GB de memoria (RAM). Si no funciona,
  prueba en un ordenador más nuevo.

====================================================================
ENGLISH
====================================================================

You need TWO things to open this vault:

  1. The KEY that was emailed to you.
  2. The WORDS printed on the physical card the owner gave you.

Neither one alone is enough — you need both.

STEPS

  1. You already have the files (they are in this folder).
  2. Open the kawarimi program for YOUR computer — find it in the list at
     the top ("Programs included"):
       - Windows: double-click the program ending in .exe
       - Mac:     there are two Mac programs: "Apple Silicon" for Macs from
                  2021 onwards (M1/M2/M3...) and "Intel" for older Macs;
                  if unsure, try the Apple Silicon one first.
                  Open "Terminal", drag the program into the window,
                  type a space and the word  open  and press Enter
       - Linux:   same as on Mac: open a terminal, drag the program in,
                  type a space and the word  open  and press Enter
  3. The program will ask you to:
       - Paste the KEY from the email.
       - Type the WORDS from the card.
  4. Your files will appear in the "decrypted" folder.
     Open INDEX.md first — it lists everything.

IF YOUR SYSTEM WARNS THE PROGRAM IS "NOT SAFE"

  This is expected: the program is unsigned, but it is the one the owner left you.
   - Windows (SmartScreen): click "More info", then "Run anyway".
   - Mac: when it is blocked, open System Settings > Privacy & Security,
     scroll to the message about the program, click "Open Anyway", and run
     it again. (On older Macs this also works: right-click the program >
     "Open" > confirm.)

IF THE PROGRAM CLOSES BY ITSELF OR WON'T OPEN

  It needs a computer with at least 2 GB of memory (RAM). If it doesn't work,
  try a newer computer.
`, buildDate, binaryList(binaries))
}

// releaseEmailBody is the single source of the release email, shared by the local
// trigger path and the generated cloud workflow so the two can never drift.
// silentES/silentEN state how long the owner has been silent; location and key are
// either real values (local path) or bash placeholders (workflow heredoc).
func releaseEmailBody(silentES, silentEN, location, key string) string {
	return fmt.Sprintf(`Este es un mensaje automático de la caja fuerte de información Kawarimi.
This is an automated message from the Kawarimi information vault.

--- ESPAÑOL ---

El titular no ha dado señales de vida %s, así que ahora
puedes acceder a la información que dejó preparada para ti.

IMPORTANTE: además de este correo necesitas la TARJETA física con las
palabras que el titular te entregó en su día. La clave de este correo por sí
sola no abre nada. Si no tienes la tarjeta, pregunta a la familia.

  1. Descarga el paquete (usa siempre el MÁS RECIENTE) desde:
       %s
     Dentro, el archivo INSTRUCTIONS.md indica la fecha del paquete.
  2. Descomprime el archivo .zip en una carpeta.
  3. Abre el programa kawarimi de tu ordenador (doble clic en Windows;
     en Mac/Linux, ejecútalo con la palabra  open ) y sigue las preguntas.
  4. Cuando te pida la CLAVE, pega este texto:

       %s

  5. Cuando te pida las PALABRAS, escríbelas desde la tarjeta física.
  6. Tus archivos aparecerán en la carpeta "decrypted"; abre INDEX.md primero.

--- ENGLISH ---

The owner has not checked in %s, so you may now access
the information they prepared for you.

IMPORTANT: besides this email you need the physical CARD with the words the
owner gave you. The key in this email opens nothing by itself. If you do not
have the card, ask the family.

  1. Download the package (always use the NEWEST one) from:
       %s
     Inside, INSTRUCTIONS.md shows the package date.
  2. Unzip the file into a folder.
  3. Open the kawarimi program for your computer (double-click on Windows;
     on Mac/Linux run it with the word  open ) and follow the prompts.
  4. When it asks for the KEY, paste this text:

       %s

  5. When it asks for the WORDS, type them from the physical card.
  6. Your files will appear in the "decrypted" folder; open INDEX.md first.
`, silentES, location, key, silentEN, location, key)
}

// ReleaseEmailBody returns the body of the email a recipient receives when the
// dead man's switch fires (local trigger path). packageLocation is where to
// download the package; dmsKey is the key value to paste.
func ReleaseEmailBody(packageLocation, dmsKey string) string {
	return releaseEmailBody("en el plazo previsto", "within the expected time", packageLocation, dmsKey)
}

// ReleaseEmailBodyWorkflow returns the same email for embedding in the generated
// GitHub workflow's bash heredoc: bash placeholders pass through verbatim and
// accents are stripped so the raw heredoc stays ASCII (conservative against MIME
// mangling — the workflow's curl mailer uploads the file as-is).
func ReleaseEmailBodyWorkflow() string {
	return stripAccents(releaseEmailBody("en $DAYS dias", "for $DAYS days",
		"$VAULT_PACKAGE_LOCATION", "$DMS_KEY"))
}

var accentReplacer = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"Á", "A", "É", "E", "Í", "I", "Ó", "O", "Ú", "U", "Ü", "U", "Ñ", "N",
	"¿", "", "¡", "", "—", "-",
)

func stripAccents(s string) string {
	return accentReplacer.Replace(s)
}

// VaultReadme returns the README.md written into a vault directory. It no longer
// tells recipients to use the `age` CLI (which cannot open a V2/V4 vault); it
// points them at the package instructions instead.
func VaultReadme() string {
	return `# Información importante / Important information

ESPAÑOL: Esta carpeta es una caja fuerte cifrada. No se abre con programas
normales. Para abrirla necesitas el programa "kawarimi", la CLAVE que llega por
correo electrónico y la TARJETA física con las palabras. Consulta el archivo
INSTRUCTIONS.md del paquete, o ejecuta:  kawarimi open

ENGLISH: This folder is an encrypted vault. It cannot be opened with ordinary
tools. To open it you need the "kawarimi" program, the KEY sent by email, and the
physical CARD with the words. See INSTRUCTIONS.md in the package, or run:
  kawarimi open
`
}

// VaultDecryptInstructions returns DECRYPT_INSTRUCTIONS.md written into a vault
// directory. Kept for backward compatibility with the file name; content now
// matches the canonical package instructions rather than the old age-CLI steps.
func VaultDecryptInstructions() string {
	return PackageInstructions(nil, "see the package")
}
