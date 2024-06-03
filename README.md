## Where are you at?

A simple question I have to ask my guildies every night, because they hang out in multiple different discords for no real reason.

The solution? Make a bot which tracks where they are, and shows a discord-ish website for an easy overview.

Setting it up:

1.  `mv env.json.tmpl env.json`
2.  Add your client secret and your optional main guild id to the `env.json`.
3.  Add your bot to the server you want to track: https://discord.com/oauth2/authorize?client_id=your_client_id

The main guild id in the env is just if you want to filter to see friends who exists in a specific discord (since there might be others you don't know in the scattered servers).

You can easily get the guild ID by e.g. copying a url from a channel and look at the first id in the url. E.g. for https://discord.com/channels/x/y the guild id is x.
