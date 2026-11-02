// The query application only reads the index; root credentials stay in MongoDB.
db.getSiblingDB('mifolyo_index').createUser({
    user: 'mifolyo_query',
    pwd: process.env.MONGO_QUERY_PASSWORD,
    roles: [{role: 'read', db: 'mifolyo_index'}],
});
