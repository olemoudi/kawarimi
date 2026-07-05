# kawarimi

[English](README.md) | **Español**

[![CI](https://github.com/olemoudi/kawarimi/actions/workflows/ci.yml/badge.svg)](https://github.com/olemoudi/kawarimi/actions/workflows/ci.yml)
[![coverage](https://raw.githubusercontent.com/olemoudi/kawarimi/badges/coverage.svg)](https://github.com/olemoudi/kawarimi/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/olemoudi/kawarimi?include_prereleases&color=9a6425)](https://github.com/olemoudi/kawarimi/releases/latest)
[![license](https://img.shields.io/badge/license-MIT-555)](LICENSE)

Una **caja fuerte digital cifrada para tu legado**. Guardas instrucciones,
credenciales y documentos cifrados mientras vives; si mueres o quedas
permanentemente incapacitado, un interruptor de hombre muerto (*dead man's
switch*) entrega a un familiar exactamente lo que necesita para abrir el
paquete — y nada antes de ese momento.

Dos objetivos guían cada decisión de diseño:

1. **Ninguna revelación no autorizada mientras estés vivo y capaz.**
2. **Fácil para quien lo recibe** — un familiar sin conocimientos técnicos debe
   poder abrir el paquete con un asistente guiado en lenguaje llano (español e
   inglés).

![Consola de kawarimi](docs/img/console-dark.png)

## Descargar

Descarga el archivo para tu ordenador desde la
[**última versión publicada**](https://github.com/olemoudi/kawarimi/releases/latest):

| Tu ordenador | Archivo |
| --- | --- |
| Windows | `kawarimi-windows-amd64.exe` |
| Mac con Apple Silicon (2021 en adelante) | `kawarimi-darwin-arm64` |
| Mac con Intel | `kawarimi-darwin-amd64` |
| Linux | `kawarimi-linux-amd64` o `kawarimi-linux-arm64` |

Los programas no están firmados (no hay un certificado corporativo detrás de
este proyecto), así que la primera ejecución requiere un paso extra:

- **Windows** — si SmartScreen avisa, pulsa **Más información → Ejecutar de
  todas formas**.
- **macOS** — haz clic derecho sobre el archivo y elige **Abrir**, y confirma.
  Si macOS aún se niega, abre **Ajustes del Sistema → Privacidad y seguridad**,
  baja hasta el aviso sobre kawarimi y pulsa **Abrir de todas formas**. (La
  primera vez puede que también necesites ejecutar
  `chmod +x kawarimi-darwin-arm64` desde Terminal.)

Integridad: cada versión incluye un `checksums.txt`; verifícalo con
`sha256sum -c checksums.txt --ignore-missing`.

## Primeros pasos (titular)

Ejecuta el programa que descargaste. En un equipo nuevo abre el **asistente de
configuración** en tu navegador (español/inglés) y te guía por todo:

1. Crea la caja fuerte y elige una contraseña.
2. Guarda tus tres secretos (mnemónico, código de recuperación y tarjeta del
   destinatario) — se muestran una sola vez.
3. Configura el interruptor: correo, quién recibe la caja fuerte y el
   calendario de avisos y entrega.
4. Arma la nube: pega un token de GitHub y kawarimi crea el repositorio privado
   del interruptor y configura sus secretos por ti.
5. Genera el paquete para los destinatarios (un zip con la caja fuerte cifrada
   y los programas para cada plataforma) y súbelo a un lugar accesible.
6. Entrega a cada destinatario su tarjeta impresa. Después solo tienes que dar
   señales de vida desde el panel (o `kawarimi checkin`) según tu calendario.

¿Prefieres la terminal? El mismo flujo existe como comandos:

```sh
kawarimi init                   # crea la caja fuerte (muestra tus secretos UNA VEZ)
kawarimi add note "Cuentas del banco"
kawarimi switch setup           # interruptor: SMTP, destinatarios, calendario
kawarimi switch verify          # confirma que está armado y al día
kawarimi checkin                # repítelo según tu calendario
kawarimi package build          # genera el paquete para los destinatarios
```

## Para el destinatario

Cuando el interruptor se dispara, el destinatario recibe un correo con una
**clave**. Entonces:

1. Descarga el paquete y lo descomprime.
2. Ejecuta el programa `kawarimi` incluido — en Windows basta con **doble
   clic**; en macOS/Linux se ejecuta `./kawarimi-<so> open`. (El propio
   `kawarimi` arranca el asistente automáticamente cuando está junto a un
   paquete.)
3. Pega la **clave** del correo y escribe las **palabras** de la tarjeta.
4. Los archivos descifrados aparecen en la carpeta `decrypted/`; `INDEX.md` es
   el índice de todo.

Todo el texto orientado al destinatario (instrucciones del paquete, correo de
entrega, asistente) es bilingüe: **primero español, luego inglés**.

## Mantener kawarimi actualizado

kawarimi se actualiza solo. Cuando hay una versión nueva, la consola muestra un
aviso **Actualización disponible** (y los comandos muestran una línea de aviso); con
un clic, o con `kawarimi update`, se descarga la nueva versión, se **verifica su
firma Ed25519 y su checksum**, y se reemplaza el programa. Reinícialo y ya estás en
la versión nueva. Solo el programa del titular se autoactualiza — el destinatario
que abre un paquete nunca lo hace.

Dos cosas migran de forma automática o con un aviso:

- **Tu caja fuerte.** Si una versión nueva necesita un formato en disco más reciente,
  kawarimi actualiza tu caja fuerte al abrirla, guardando una copia de seguridad con
  fecha en `~/.kawarimi/backups/`. No tienes que hacer nada.
- **Tu interruptor en la nube.** La automatización del interruptor se sube a GitHub
  una sola vez, así que una mejora posterior (o una corrección de seguridad) no llega
  sola. Tras actualizar, ejecuta `kawarimi switch verify`; si dice que la
  automatización está anticuada, ejecuta `kawarimi switch seed` para renovarla. Si
  cambiaste el contenido de la caja fuerte, vuelve a ejecutar `kawarimi package
  build` y súbelo de nuevo.

## Cómo funciona

La caja fuerte se cifra con [age](https://github.com/FiloSottile/age) (X25519).
La clave maestra se envuelve en varias ranuras: tu **contraseña + clave del
dispositivo**, un **código de recuperación** y un **mnemónico de 8 palabras**
(tu copia en papel).

Para el destinatario, kawarimi usa una **división de claves** de forma que
ningún secreto por sí solo — y ningún secreto que tengas que entregar por
adelantado — pueda abrir la caja fuerte antes de que el interruptor se dispare.
Hacen falta tres cosas, en manos de tres partes/lugares distintos:

| Secreto | Quién lo tiene | Cuándo lo recibe el destinatario |
| --- | --- | --- |
| **Carga sellada** (`sealed_payload.age`) | viaja dentro del paquete (público) | ya está en la descarga |
| **Clave DMS** (32 bytes aleatorios) | el interruptor de hombre muerto | por correo cuando el interruptor se dispara |
| **Frase del destinatario** (6 palabras) | una tarjeta física que tú le das | en mano, de tu parte |

La carga sellada es el mnemónico de 8 palabras cifrado bajo *ambas* cosas: la
clave DMS y la frase del destinatario. Un paquete filtrado + la tarjeta no
pueden abrirla (falta la clave DMS); una clave DMS filtrada tampoco (falta la
tarjeta). Hacen falta las dos, y la clave DMS solo se envía cuando dejas de dar
señales de vida.

Hay **dos canales de entrega**:

- **Nube (GitHub Actions)** — el disparador real póstumo. Un workflow en un
  repositorio dedicado lee el latido que envías con cada check-in y manda por
  correo la clave DMS a tus destinatarios cuando llevas demasiado tiempo sin
  aparecer. Funciona aunque tu máquina esté apagada.
- **Local (temporizador systemd)** — te envía recordatorios mientras tu máquina
  funciona. Por defecto no guarda ninguna clave («solo nube») y nunca realiza
  la entrega final.

## Modelo de amenazas (resumen)

- **Atacante con el paquete público + la tarjeta, titular vivo:** no puede
  abrirla — la clave DMS no se ha enviado.
- **Atacante que compromete la máquina del titular:** en el modo por defecto
  «solo nube» la máquina no guarda la clave DMS, así que no la consigue. (Usa
  cifrado de disco completo de todos modos.)
- **Atacante que intercepta solo el correo de entrega (clave DMS):** no puede
  abrir la caja fuerte sin la tarjeta física.
- **Disparo en falso que llega a los destinatarios previstos:** gravedad baja —
  la clave por sí sola no abre nada. Rota con `kawarimi switch rekey` solo si
  la clave llegó a alguien más.

El modelo de amenazas completo — incluido el presupuesto de 100.000 $/año
contra el que está calibrado el medidor de fortaleza de contraseñas — está en
[THREAT_MODEL.md](THREAT_MODEL.md) (en inglés).

## Restricciones operativas

- El repositorio del interruptor debe ser un repositorio de GitHub **separado,
  privado y vacío** (sin README) para que el workflow quede en la rama `main` y
  su programación funcione.
- Mantén `FinalDays` bien por debajo de ~60 — GitHub desactiva los workflows
  programados tras ~60 días sin actividad; `switch verify` avisa si es
  demasiado alto.
- La clave SSH usada para los check-ins no debe tener contraseña (el
  temporizador systemd funciona desatendido).

## Compilar desde el código

```sh
make build       # binario local, versión estampada desde git (CGO_ENABLED=0)
make test        # go test -short ./...
make cross       # binarios para todas las plataformas en dist/
```

Requiere Go 1.25+. Las dependencias están vendorizadas: compila sin conexión.

## Más documentación

- [ARCHITECTURE.md](ARCHITECTURE.md) — diseño técnico completo (división de
  claves, ranuras, motor del interruptor, GUI, modelo de durabilidad). *(En
  inglés.)*
- [docs/usage-flow.md](docs/usage-flow.md) — diagrama del ciclo de vida completo.
- [docs/reliability-review.md](docs/reliability-review.md) — revisión de modos
  de fallo y el endurecimiento aplicado. *(En inglés.)*

## Licencia

MIT — ver [LICENSE](LICENSE).
