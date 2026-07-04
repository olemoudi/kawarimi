package recipient

// messages holds the recipient-wizard UI strings for one language.
type messages struct {
	intro      string
	promptKey  string
	badKey     string
	promptPass string
	decrypting string
	tryAgain   string
	gaveUp     string
	success    string // one %s for the output path
	pressEnter string
	noVault    string
	lowMemory  string // one %d for available MiB
}

func messagesFor(lang string) messages {
	if lang == "en" {
		return messages{
			intro: "\nTo open the vault you need TWO things:\n" +
				"  1) the KEY from the email\n" +
				"  2) the WORDS from the physical card\n",
			promptKey:  "\nPaste the KEY from the email and press Enter:\n> ",
			badKey:     "That does not look like a valid key. Copy the WHOLE line from the email and try again.",
			promptPass: "Type the WORDS from the card (separated by spaces) and press Enter:\n> ",
			decrypting: "Opening the vault...",
			tryAgain:   "That did not work. Check the key and the words (spelling matters) and try again.",
			gaveUp: "Could not open the vault. Make sure you are using the KEY from the most recent\n" +
				"email and the correct card. If this package is an old copy, download the newest one.",
			success:    "\nDone. Your files are in:\n  %s\nOpen INDEX.md first — it lists everything.\n",
			pressEnter: "\nPress Enter to close this window...",
			noVault: "Could not find a vault here. Put this program in the same folder as the vault\n" +
				"package (the .zip you downloaded), or extract the zip first, then run it again.",
			lowMemory: "\nNote: this computer reports only about %d MB of free memory. Opening the vault\n" +
				"needs roughly 1.5 GB free and may fail here — if it does, try a computer with\n" +
				"at least 2 GB of RAM.\n",
		}
	}
	return messages{
		intro: "\nPara abrir la caja fuerte necesitas DOS cosas:\n" +
			"  1) la CLAVE del correo electrónico\n" +
			"  2) las PALABRAS de la tarjeta física\n",
		promptKey:  "\nPega la CLAVE del correo y pulsa Intro:\n> ",
		badKey:     "Eso no parece una clave válida. Copia la línea ENTERA del correo e inténtalo otra vez.",
		promptPass: "Escribe las PALABRAS de la tarjeta (separadas por espacios) y pulsa Intro:\n> ",
		decrypting: "Abriendo la caja fuerte...",
		tryAgain:   "No ha funcionado. Revisa la clave y las palabras (cuida la ortografía) e inténtalo otra vez.",
		gaveUp: "No se pudo abrir la caja fuerte. Asegúrate de usar la CLAVE del correo más reciente\n" +
			"y la tarjeta correcta. Si este paquete es una copia antigua, descarga el más nuevo.",
		success:    "\nListo. Tus archivos están en:\n  %s\nAbre primero INDEX.md: es el índice de todo.\n",
		pressEnter: "\nPulsa Intro para cerrar esta ventana...",
		noVault: "No se encontró ninguna caja fuerte aquí. Pon este programa en la misma carpeta que\n" +
			"el paquete (el .zip que descargaste), o descomprime el zip primero, y vuelve a ejecutarlo.",
		lowMemory: "\nNota: este equipo indica solo unos %d MB de memoria libre. Abrir la caja fuerte\n" +
			"necesita aproximadamente 1,5 GB libres y podría fallar aquí — si falla, prueba en un\n" +
			"equipo con al menos 2 GB de RAM.\n",
	}
}
