---
title: Cómo trata Earful sus datos
short_title: Confianza
sections: cards
hash: sha256-1228a9fb0c7004a2fd246b565c41c23dbc44256edae5c6b4e6e97c45583a579a
last_update: 2026-09-30
source_hash: sha256-2cfa7afa4d9b79f7ee9557bfcd37ec9054a2fd8b19f5c1a0c449ef06cc4c00ea
---

Esta página es una traducción. El texto de referencia es la [versión en inglés](/trust?lang=en); si las dos difieren, vale lo que dice aquella.

Esta página describe {{if .Instance}}{{.Instance}}{{else}}esta instalación{{end}}. Earful es de código abierto (AGPL 3.0), de modo que todo lo que aquí se afirma puede comprobarse en el código que sirve esta página.

## Su voz nunca se guarda

Puede responder hablando. El audio se convierte en texto y se descarta en la misma petición: nunca se escribe en disco, en una base de datos, en registros ni en ninguna copia de seguridad, en ningún entorno. Lo que se conserva es la transcripción que usted leyó y corrigió antes de enviarla.

No hay función de reproducción, porque no hay nada que reproducir. Si eso cambiara alguna vez, sería una decisión de producto distinta, anunciada aquí y consultada antes con usted; no algo que pudiera ocurrir sin aviso con un audio que hubiéramos guardado entretanto.

## Anónima quiere decir anónima

Quien crea una encuesta decide al crearla si es anónima, y esa decisión no puede cambiarse después: lo impide la base de datos, no solo la aplicación.

Un envío anónimo no lleva correo electrónico, ni dirección IP, ni datos del dispositivo. Esas columnas no existen cerca de ningún envío, de modo que ninguna consulta ni ningún error puede rellenarlas sin que se note. Añadirlas exigiría un cambio deliberado en la base de datos, a la vista de todos, en un repositorio de código abierto.

Quienes crean encuestas sí ven algunos totales sobre su audiencia, nunca unidos a un envío:

- Familia de navegador, tipo de dispositivo y país, ocultos para cualquier grupo de menos de cinco personas. El país se determina en nuestro propio servidor con una base de datos sin conexión, y la dirección IP se descarta de inmediato.
- Cuántas veces se abrió la encuesta y cuántas respuestas se enviaron, por día. Los totales de audiencia nunca se separan por día.

## En la página de una encuesta no se ejecuta nada de terceros

Responder una encuesta no carga analíticas, ni fuentes, ni gestores de etiquetas, ni scripts de una CDN: nada más que esta aplicación. La comprobación contra bots es nuestra, está en la propia página y no identifica a nadie. Una prueba impide publicar el programa si aparece un origen de terceros en una página de encuesta.

## El idioma de la interfaz

Si elige un idioma para la interfaz, su elección se guarda en una cookie de su navegador, llamada `interface_lang`, durante un año. No se crea ni se lee en la página de una encuesta: allí el idioma se elige en la dirección de la página y no se guarda en ningún lugar.

## Dónde están los datos y quién los toca

{{if .NoProcessors}}{{if .Region}}Alojado en {{.Region}}. {{end}}No interviene ninguna empresa externa: esta instalación funciona por completo en la infraestructura de su propio operador.{{else}}{{if .Region}}Alojado en {{.Region}}. Todas las empresas que intervienen:{{else}}Todas las empresas que intervienen en el funcionamiento de esta instalación:{{end}}{{end}}

{{- if .GoogleCloud}}
- **Google Cloud** aloja la aplicación, la base de datos, las copias de seguridad y los registros, así que puede ver todo lo que guarda el servicio. Región `europe-west4`.
{{- end}}
{{- if .Brevo}}
- **Brevo** envía los enlaces de acceso y las invitaciones a encuestas. Ve los correos de titulares de cuentas y de participantes invitados. UE (Francia).
{{- end}}
{{- if eq .AI "vertex"}}
- **Google Vertex AI** transcribe las respuestas habladas y redacta preguntas, resúmenes y traducciones. Ve el audio en tránsito, que nunca se guarda, y el texto de preguntas y respuestas. {{if eq .VertexLocation "eu"}}UE, procesado solo en Estados miembros de la UE.{{else if eq .VertexLocation "us"}}Estados Unidos.{{else}}Región `{{.VertexLocation}}`.{{end}}
{{- end}}
{{- if eq .AI "openai"}}
- **Un servicio de IA elegido por el operador** transcribe, redacta, resume y traduce. Ve el audio en tránsito, que nunca se guarda, y el texto de preguntas y respuestas, allí donde lo haya dispuesto el operador de esta instalación.
{{- end}}
{{- if .GoogleLogin}}
- **Google Identity** inicia la sesión de quienes eligen Google. Ve su correo electrónico y el identificador de su cuenta de Google.
{{- end}}

{{if not .NoProcessors}}Alojar Earful por su cuenta los elimina a todos: funciona con su propio Postgres, su propio servidor SMTP y, si quiere funciones de IA, su propio modelo.{{end}}

## Lo que no podemos prometer

{{if .GoogleCloud}}Nuestra infraestructura está en la UE, pero la empresa matriz de Google Cloud es estadounidense, y la ley de Estados Unidos alcanza a las empresas estadounidenses estén donde estén sus servidores. Alojar en la UE reduce ese riesgo; no lo elimina. Preferimos decirlo antes que dar a entender una garantía que no podemos ofrecer.{{end}}

Los datos eliminados se retiran de los sistemas en uso de inmediato y se borran definitivamente en un plazo de 30 días. Las copias de seguridad se conservan 30 días y son inmutables a propósito, lo que significa que una supresión es plenamente efectiva en 30 días, no al instante. Es el equilibrio habitual frente al riesgo de perderlo todo por un ataque de secuestro de datos, y creemos que es el correcto.

## Puede irse

Un botón exporta todo lo que contiene un espacio de trabajo (cada encuesta, versión, pregunta y envío) como JSON documentado y archivos CSV. El formato está publicado y tiene versiones, y el propio Earful es AGPL 3.0, de modo que puede ejecutar el mismo programa por su cuenta y llevarse sus datos.

[Código fuente](https://github.com/TryEarful/earful) · [Formato de exportación](https://github.com/TryEarful/earful/blob/main/docs/export-format.md)

## Contacto

{{if .ContactEmail}}Preguntas, o una solicitud sobre sus propios datos: [{{.ContactEmail}}](mailto:{{.ContactEmail}}).{{else}}Esta instalación no ha publicado una dirección de contacto. Pregunte a quien le envió la encuesta: es quien decide qué ocurre con sus respuestas, y puede dirigirse a las personas que administran esta instalación.{{end}}

***

Geolocalización de IP: {{.GeoSource}} · [db-ip.com]({{.GeoAttributionURL}})
