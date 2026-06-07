import pymongo
import logging
import time

from typing import Optional, List
from pymongo import UpdateOne

from models.backlinks import Backlinks

# SETUP LOGGER
logger = logging.getLogger(__name__)
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)

# COLLECTIONS
OUTLINKS_COLLECTIONS = 'outlinks'

class MongoClient:
    def __init__(self, host='localhost', port=27017, password="", db="test", username=""):
        try:
            mongo_uri = f'mongodb://{host}:{port}/{db}'
            if username:
                mongo_uri = f'mongodb://{username}:{password}@{host}:{port}/{db}?authSource=admin'

            self.client = pymongo.MongoClient(mongo_uri)
            self.db = self.client[db]
            self.client.admin.command("ping")
            logger.info('Successfully connected to mongo!')
        except Exception as e:
            logger.error(f'Failed to connect to mongo')
            self.client = None


    def perform_batch_operations(self, operations: List[UpdateOne], collection_name: str):
        if self.client is None:
            logger.error(f'Mongo connection not initialized')
            return None

        if not operations:
            logger.error(f'No operations to perform')
            return None

        res = self.db[collection_name].bulk_write(operations)
        return res

    # --------------------- OUTLINKS ---------------------

    def save_all_backlinks(self, backlinks: List[Backlinks]) -> Optional[int]:
        if self.client is None:
            logger.error(f'MongoDB connection not initialized')
            return None

        operations = []
        for backlink_data in backlinks:
            # logger.info(f'Saving backlinks for {backlink_data._id}')
            for link in backlink_data.links:
                save_op = UpdateOne(
                    {"_id": backlink_data._id},
                    {
                        "$addToSet": {
                            "links": link
                        }
                    },
                    upsert=True
                )
                operations.append(save_op)

        res = self.perform_batch_operations(operations, 'backlinks')
        print(res)

    # --------------------- OUTLINKS ---------------------
