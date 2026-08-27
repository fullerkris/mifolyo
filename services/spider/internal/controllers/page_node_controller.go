package controllers

import (
	"log"

	"github.com/IonelPopJara/search-engine/services/spider/internal/crawler"
	"github.com/IonelPopJara/search-engine/services/spider/internal/database"
	"github.com/IonelPopJara/search-engine/services/spider/internal/utils"
)

type LinksController struct {
	db *database.Database
}

func NewLinksController(db *database.Database) *LinksController {
	return &LinksController{db: db}
}

func (pgc *LinksController) SaveLinks(crawcfg *crawler.CrawlerConfig) error {
	pipeline := pgc.db.Client.Pipeline()
	publicationID, err := batchPublicationID(crawcfg)
	if err != nil {
		return err
	}

	log.Printf("Saving backlinks...\n")
	data := crawcfg.Backlinks
	count := len(data)
	for key, backlinks := range data {
		for _, link := range backlinks.GetLinks() {
			pipeline.SAdd(pgc.db.Context, utils.BacklinksPrefix+":"+key, link)
		}
	}

	log.Printf("Saving outlinks...\n")
	data = crawcfg.Outlinks
	count += len(crawcfg.Pages)
	for key := range crawcfg.Pages {
		outlinks := data[key]
		if outlinks == nil {
			continue
		}
		for _, link := range outlinks.GetLinks() {
			pipeline.SAdd(pgc.db.Context, outlinksPublicationKey(publicationID, key), link)
		}
	}

	if _, err := pipeline.Exec(pgc.db.Context); err != nil {
		return controllerError("persist crawl links", err)
	}
	log.Printf("Successfully written %d entries to the db!", count)
	return nil
}
