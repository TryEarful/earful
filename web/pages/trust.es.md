---
title: Cómo trata Earful sus datos
short_title: Confianza
sections: cards
hash: sha256-f858b6ed8fd7cd14abfd9f6ab38efbb0f944f7722e01284c4cd5920b35fdfa1d
last_update: 2026-09-29
source_hash: sha256-8523957e7bd32bbea8337d5c0487c55d42a200a5422341204a95a35185f31e0f
---

Esta página es una traducción. El texto de referencia es la [versión en inglés](/trust?lang=en); si las dos difieren, vale lo que dice aquella.

Esta página describe {{if .Instance}}{{.Instance}}{{else}}esta instalación{{end}}. Earful es de código abierto (AGPL-3.0), de modo que todo lo que aquí se afirma puede comprobarse en el código que sirve esta página.

## Su voz nunca se guarda

Puede responder hablando. El audio se convierte en texto y se descarta en la misma petición: nunca se escribe en disco, en una base de datos, en registros ni en ninguna copia de seguridad, en ningún entorno. Lo que se conserva es la transcripción que usted leyó y corrigió antes de enviarla.

No hay función de reproducción, porque no hay nada que reproducir. Si eso cambiara alguna vez, sería una decisión de producto distinta, anunciada aquí y consultada antes con usted; no algo que pudiera ocurrir sin aviso con un audio que hubiéramos guardado entretanto.

## Anónima quiere decir anónima

Quien crea una encuesta decide al crearla si es anónima, y esa decisión no puede cambiarse después: lo impide la base de datos, no solo la aplicación.

Un envío anónimo no lleva correo electrónico, ni dirección IP, ni datos del dispositivo. Esas columnas no existen cerca de ningún envío, de modo que ninguna consulta ni ningún error puede rellenarlas sin que se note. Añadirlas exigiría un cambio deliberado en la base de datos, a la vista de todos, en un repositorio de código abierto.

Quienes crean encuestas sí ven recuentos generales sobre su audiencia (familia de navegador, tipo de dispositivo y país) como totales de la encuesta, nunca unidos a un envío, y ocultos por completo para cualquier grupo de menos de cinco personas. El país se determina en nuestro propio servidor con una base de datos sin conexión, y la dirección IP se descarta de inmediato. También se cuenta por día cuántas veces se abrió una encuesta y cuántas respuestas se enviaron, para que quien la creó pueda ver cómo le fue a lo largo de una semana; son recuentos de la encuesta, sin nada unido a ningún envío, y los recuentos de audiencia nunca se separan por día.

## En la página de una encuesta no se ejecuta nada de terceros

Responder una encuesta no carga analíticas, ni fuentes, ni gestores de etiquetas, ni scripts de una CDN: nada más que esta aplicación. La comprobación contra bots es nuestra, está en la propia página y no identifica a nadie. Una prueba impide publicar el programa si aparece un origen de terceros en una página de encuesta.

## El idioma de la interfaz

Si elige un idioma para la interfaz, su elección se guarda en una cookie de su navegador, llamada `interface_lang`, durante un año. No se crea ni se lee en la página de una encuesta: allí el idioma se elige en la dirección de la página y no se guarda en ningún lugar.

## Dónde están los datos y quién los toca

{{if .Region}}Alojado en {{.Region}}. Estas son todas las empresas que intervienen:{{else}}Estas son todas las empresas que intervienen en el funcionamiento de esta instalación:{{end}}

| Encargado | Para qué | Qué ve | Dónde |
|---|---|---|---|
{{- if .GoogleCloud}}
| Google Cloud | Alojamiento: la aplicación, la base de datos, las copias de seguridad y los registros | Todo lo que guarda el servicio | europe-west4 |
{{- end}}
{{- if .Brevo}}
| Brevo | Envío de enlaces de acceso e invitaciones a encuestas | Correos electrónicos de titulares de cuentas y de participantes invitados | UE (Francia) |
{{- end}}
{{- if eq .AI "vertex"}}
| Google Vertex AI | Transcribir respuestas habladas y redactar preguntas, resúmenes y traducciones | Audio en tránsito (nunca guardado), texto de preguntas y respuestas | {{if eq .VertexLocation "eu"}}UE (multirregión de Google Cloud: procesado solo en Estados miembros de la UE){{else if eq .VertexLocation "us"}}Estados Unidos (multirregión de Google Cloud){{else}}{{.VertexLocation}}{{end}} |
{{- end}}
{{- if eq .AI "openai"}}
| Servicio de IA configurado por el operador | Transcripción, redacción, resúmenes y traducciones | Audio en tránsito (nunca guardado), texto de preguntas y respuestas | Donde lo haya dispuesto el operador de esta instalación |
{{- end}}
{{- if .GoogleLogin}}
| Google Identity | Inicio de sesión, solo para quien elige Google | Correo electrónico e identificador de la cuenta de Google | Global |
{{- end}}
{{- if .NoProcessors}}
| Nadie | Esta instalación funciona por completo en la infraestructura de su propio operador | — | — |
{{- end}}

Alojar Earful por su cuenta los elimina a todos: funciona con su propio Postgres, su propio servidor SMTP y, si quiere funciones de IA, su propio modelo.

## Lo que no podemos prometer

Nuestra infraestructura está en la UE, pero la empresa matriz de Google Cloud es estadounidense, y la ley de Estados Unidos alcanza a las empresas estadounidenses estén donde estén sus servidores. Alojar en la UE reduce ese riesgo; no lo elimina. Preferimos decirlo antes que dar a entender una garantía que no podemos ofrecer.

Los datos eliminados se retiran de los sistemas en uso de inmediato y se borran definitivamente en un plazo de 30 días. Las copias de seguridad se conservan 30 días y son inmutables a propósito, lo que significa que una supresión es plenamente efectiva en 30 días, no al instante. Es el compromiso habitual frente al riesgo de perderlo todo por un ataque de secuestro de datos, y creemos que es el correcto.

## Puede irse

Un botón exporta todo lo que contiene un espacio de trabajo (cada encuesta, versión, pregunta y envío) como JSON documentado y archivos CSV. El formato está publicado y tiene versiones, y el propio Earful es AGPL-3.0, de modo que puede ejecutar el mismo programa por su cuenta y llevarse sus datos.

[Código fuente](https://github.com/TryEarful/earful) · [Formato de exportación](https://github.com/TryEarful/earful/blob/main/docs/export-format.md)

## Atribución

{{.GeoAttribution}} — [db-ip.com]({{.GeoAttributionURL}})

***

{{if .ContactEmail}}Preguntas, o una solicitud sobre sus propios datos: [{{.ContactEmail}}](mailto:{{.ContactEmail}}).{{else}}Esta instalación no ha publicado una dirección de contacto. Pregunte a quien le envió la encuesta: es quien decide qué ocurre con sus respuestas, y puede dirigirse a las personas que administran esta instalación.{{end}}
