package hn

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/mseshachalam/x/app"
	"github.com/mseshachalam/x/dbp"
	"github.com/mseshachalam/x/encrypt"
	"github.com/mseshachalam/x/util"
)

// HackerNewsMaintainer implements Maintainer
type HackerNewsMaintainer struct {
	PediodicBringer *HackerNewsPeriodicBringer
	Storage         *sql.DB
	Ctx             context.Context
	Key             *[32]byte
}

// Maintain takes care of storage and updates to items
func (hnm *HackerNewsMaintainer) Maintain() {
	for bringer := range hnm.PediodicBringer.Bring() {
		items, err := bringer.Bring()
		if err != nil {
			log.Println(err)
		}

		var ids []int
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		// Update items to latest timestamp
		err = dbp.UpdateItemsAddedTimeToNow(hnm.Storage, ids)
		if err != nil {
			log.Println(err)
		}

		thirtyTwoHrsBack := time.Now().Add(-4 * app.EightHrs)
		ids, err = dbp.SelectItemsIdsBefore(hnm.Storage, thirtyTwoHrsBack.Unix())
		if err != nil {
			log.Println(err)
		}

		var olderItemsIDsNotInTop []int
		for _, id := range ids {
			there := false
			for _, it := range items {
				if id == it.ID {
					there = true
					break
				}
			}

			if !there {
				olderItemsIDsNotInTop = append(olderItemsIDsNotInTop, id)
			}
		}

		err = dbp.DeleteItemsWith(hnm.Storage, olderItemsIDsNotInTop)
		if err != nil {
			log.Println(err)
		}

		eightHrsBack := time.Now().Add(-1 * app.EightHrs)
		itemsIDsFromLastEightHrs, err := dbp.SelectItemsIDsAfter(hnm.Storage, eightHrsBack.Unix())
		if err != nil {
			log.Println(err)
		}

		var olderItemsIDsInTop []int
		for _, id := range itemsIDsFromLastEightHrs {
			there := false
			for _, topIt := range items {
				if id == topIt.ID {
					there = true
					break
				}
			}
			if there {
				olderItemsIDsInTop = append(olderItemsIDsInTop, id)
			}
		}

		updatedItems, err := FetchHNStoriesOf(hnm.Ctx, olderItemsIDsInTop)
		if err != nil {
			log.Println(err)
		}

		for _, updatedItem := range updatedItems {
			there := false
			for _, it := range items {
				if it.ID == updatedItem.ID {
					there = true
					break
				}
			}
			if !there {
				items = append(items, updatedItem)
			}
		}

		var itemIDs []int
		for _, it := range items {
			itemIDs = append(itemIDs, it.ID)
		}

		existingItems, err := dbp.SelectExistingPropsOfItemsByIDsAsc(hnm.Storage, itemIDs)
		if err != nil {
			log.Println(err)
		}
		for _, eit := range existingItems {
			for _, it := range items {
				if eit.ID != it.ID {
					continue
				}

				it.URL = eit.URL
				it.DiscussLink = eit.DiscussLink
				it.Domain = eit.Domain
				it.Description = eit.Description
				it.EncryptedURL = eit.EncryptedURL
				it.EncryptedDiscussLink = eit.EncryptedDiscussLink

				break
			}
		}

		idsToURLs := make(map[int]string)
		// visit the link with lynx and update description
		for _, it := range items {
			if it.Description == "" {
				idsToURLs[it.ID] = it.URL
			}

			if it.URL == "" {
				it.URL = fmt.Sprintf(PostLinkURL, it.ID)
			}
			if it.DiscussLink == "" {
				it.DiscussLink = fmt.Sprintf(PostLinkURL, it.ID)
			}
			if it.Domain == "" {
				domain, err := util.URLToDomain(it.URL)
				if err == nil {
					it.Domain = domain
				}
			}
			if it.EncryptedURL == "" {
				link := it.URL
				if link == "" {
					link = it.DiscussLink
				}
				h, _ := encrypt.EncAndHex(link, hnm.Key)
				it.EncryptedURL = h
			}
			if it.EncryptedDiscussLink == "" {
				h, _ := encrypt.EncAndHex(it.DiscussLink, hnm.Key)
				it.EncryptedDiscussLink = h
			}
		}

		err = dbp.InsertOrReplaceItems(hnm.Storage, items)
		if err != nil {
			log.Println(err)
		}
	}
}