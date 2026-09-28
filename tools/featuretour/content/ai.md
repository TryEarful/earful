# What the stand-in model says

The mock model server answers from this file. Sections are `## insights: <story>` (the Insight Summary for that story, plain text in the shape the app's prompt asks for), `## translations: <language code>` (a yaml mapping from source text to its translation; anything not listed comes back unchanged), and `## transcript` (what every spoken answer transcribes to). Question drafts are not here: the mock builds them from the questions in surveys.yaml, so what the model "drafts" is exactly what the seed stage later answers.

## insights: restaurant

Themes

- The pasta is the reason people come. Around half of the open answers name a specific dish, with the cacio e pepe and the pappardelle mentioned most often.
- Service is warm but slow on weekends. Most of the "one thing we could do better" answers are about waiting: for a table, for the bill, for a second drink.
- Guests want a vegetarian main. The menu wishlist is led by more vegetarian dishes and natural wines; brunch and a tasting menu trail well behind.
- Word of mouth is the main channel. "A friend told me" beats every online source combined, though Instagram is the leading source among guests who visited on Saturday.

Patterns

- Weekend visitors rate service about a point lower than weekday visitors, while food ratings hold steady across the week.
- The handful of answers written in Spanish and Italian are as positive as the English ones and mention the staff by name more often.
- Guests who found the restaurant on Google Maps are the least likely to say they would come back; their comments mention noise and the wait more than the food.

Representative quotes

- "The cacio e pepe was the best I've had outside Rome."
- "It gets really loud after nine."
- "Vorrei un piatto vegetariano vero, non un contorno."
- "Il tiramisù è spettacolare, torneremo sicuramente."
- "Loud, cramped, but honestly the food made up for it."

What to look at next

- Read the Friday and Saturday answers separately from the rest of the week; the service complaints cluster there and may point at staffing rather than process.
- Trial one vegetarian main for a month and ask again; a third of the wishlist votes are for it.
- The sample of Google Maps arrivals is small (fewer than ten), so treat the come-back signal as a hint to check, not a conclusion.

## insights: coffee

Themes

- Discovery is the product. Most subscribers say they joined to try new origins, and the best-cup stories are about a specific bag: the Ethiopian natural and the Colombian washed lots come up again and again.
- Freshness is a strength. Roast-to-door freshness is rated highly and several answers mention the roast date on the bag as the reason they trust the subscription.
- Pour-over and espresso dominate at home, and the two groups want different things: espresso drinkers ask for grind-per-delivery, pour-over drinkers ask for tasting notes and brew recipes.
- Delivery timing is the main irritation. The one-thing-to-change answers are led by unpredictable delivery days and boxes left in the rain, not by price.

Patterns

- Subscribers who brew with an espresso machine answer "yes" to choosing grind size almost unanimously; French press and drip users mostly do not mind.
- Light-roast drinkers write the longest best-cup stories and give the highest recommendation scores.
- Answers that mention a gift subscription are more lukewarm on price and less likely to recommend, which suggests the gifted bags do not land with the right roast profile.

Representative quotes

- "The Ethiopian natural tasted like blueberries and I was not prepared for that at 7am."
- "Let me pick the grind for my Moka pot instead of getting a bag ground for pour-over."
- "It arrived on a Tuesday, then a Friday, then a Monday. Just tell me the day."
- "The roast date is on the bag. That is why I stayed."
- "I got this as a gift and I would have picked a darker roast."

What to look at next

- Offer grind size as a per-delivery choice and watch whether the espresso segment's recommendation scores move.
- Publish a delivery-day promise; it is the cheapest fix for the most-mentioned complaint.
- Seven answers is a small sample: every pattern above is a lead to check on the next batch, not a finding.

## insights: earful

Themes

- Voice is the reason people chose Earful. Almost every answer to "what made you pick Earful" mentions spoken answers, usually together with the fact that the audio is never stored.
- Respondents talk more than they type. Several creators say the spoken answers are longer and more candid than what they used to get from typed forms.
- The AI features are used, but with care. Insight Summaries and AI-drafted questions are the most-used features after voice; a few answers say they read the summary as a starting point and then go back to the raw answers.
- Missing: branching logic and a way to share results. The "what is missing" answers cluster around conditional questions, a read-only results link and an integration with a spreadsheet.

Patterns

- Creators who run surveys weekly report the highest satisfaction and are the ones asking for branching and integrations; first-time users mostly ask for more question types.
- The rewording of the recommendation question between versions did not change the shape of the scores.

Representative quotes

- "My customers just talk. I get paragraphs where I used to get three words."
- "It is the only tool where I can honestly tell people the recording does not exist."
- "I need to skip questions based on an earlier answer."
- "A share link for results so I stop screenshotting the page."

What to look at next

- Branching logic is the most requested feature and comes from the most engaged users; it is worth scoping.
- The sample is small (a few dozen responses), so read the satisfaction score as a direction rather than a number.

## translations: es

```yaml
"What did you enjoy most about your visit tonight?": "¿Qué fue lo que más disfrutó de su visita de esta noche?"
"How would you rate the food?": "¿Cómo calificaría la comida?"
"How was the service?": "¿Qué le pareció el servicio?"
"How did you hear about us?": "¿Cómo nos conoció?"
"What would you like to see on the menu?": "¿Qué le gustaría ver en la carta?"
"Which day did you visit?": "¿Qué día nos visitó?"
"Would you come back?": "¿Volvería?"
"How likely are you to recommend us to a friend?": "¿Qué tan probable es que nos recomiende a un amigo?"
"One thing we could do better?": "¿Algo que podríamos hacer mejor?"
"A friend told me": "Me lo recomendó un amigo"
"Walked past": "Pasaba por delante"
"Instagram": "Instagram"
"Google Maps": "Google Maps"
"I've been coming for years": "Vengo desde hace años"
"More vegetarian dishes": "Más platos vegetarianos"
"A tasting menu": "Un menú degustación"
"Weekend brunch": "Brunch de fin de semana"
"Natural wines": "Vinos naturales"
"Gluten-free pasta": "Pasta sin gluten"
"Monday": "Lunes"
"Tuesday": "Martes"
"Wednesday": "Miércoles"
"Thursday": "Jueves"
"Friday": "Viernes"
"Saturday": "Sábado"
"Sunday": "Domingo"
```

## translations: en

```yaml
"Il tiramisù è spettacolare, torneremo sicuramente.": "The tiramisu is spectacular, we will definitely be back."
"La pasta estaba perfecta y Marco fue muy atento con nosotros.": "The pasta was perfect and Marco was very attentive with us."
"Un poco de ruido, pero la comida lo compensa.": "A bit noisy, but the food makes up for it."
"Más opciones sin gluten, por favor.": "More gluten-free options, please."
"Il servizio è stato lento il sabato sera.": "Service was slow on Saturday evening."
"Ottimo vino, ottima atmosfera.": "Great wine, great atmosphere."
"El ambiente es acogedor y los precios justos.": "The atmosphere is cosy and the prices are fair."
"Vorrei un piatto vegetariano vero, non un contorno.": "I would like a real vegetarian dish, not a side."
```

## transcript

I picked Earful because my customers can just talk instead of typing. The spoken answers are longer and more honest than anything we got from forms, and I can tell people the recording is never kept.
