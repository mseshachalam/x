// Package maintainer deals with all the bringers.
package maintainer

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/mseshachalam/x/app"
	"github.com/mseshachalam/x/dbp"
	"github.com/mseshachalam/x/encrypt"
	"github.com/mseshachalam/x/util"
)

// Maintainer implements Maintainer
type Maintainer struct {
	Ctx              context.Context
	Config           *app.Config
	PeriodicBringers []app.PeriodicBringer
	Storage          *sql.DB
	Key              *[32]byte
	sync.Mutex
}

// Maintain takes care of storage and updates to items
func (m *Maintainer) Maintain() {
	var wg sync.WaitGroup
	for _, pb := range m.PeriodicBringers {
		wg.Add(1)
		go func(pb app.PeriodicBringer) {
			defer wg.Done()
			for b := range pb.Bring() {
				items, err := b.Bring(nil)
				if err != nil {
					log.Println(err)
				}

				var ids []int
				for _, item := range items {
					ids = append(ids, item.ID)
				}
				func() {
					m.Lock()
					defer m.Unlock()
					// Update items to latest timestamp
					err = dbp.UpdateItemsAddedTimeToNow(m.Storage, ids, b.GetSource())
					if err != nil {
						log.Println(err)
					}
				}()

				thirtyTwoHrsBack := time.Now().Add(-4 * app.EightHrs)
				olderItemsIDsNotInTop, err := dbp.SelectItemsIdsBeforeAndNotOf(m.Storage, thirtyTwoHrsBack.Unix(), ids, b.GetSource())
				if err != nil {
					log.Println(err)
				}

				func() {
					m.Lock()
					defer m.Unlock()
					err = dbp.DeleteItemsWith(m.Storage, olderItemsIDsNotInTop, b.GetSource())
					if err != nil {
						log.Println(err)
					}
				}()

				eightHrsBack := time.Now().Add(-1 * app.EightHrs)
				olderItemsIDsInTop, err := dbp.SelectItemsIDsAfterAndNotOf(m.Storage, eightHrsBack.Unix(), ids, b.GetSource())
				if err != nil {
					log.Println(err)
				}

				updatedItems, err := b.Bring(olderItemsIDsInTop)
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
						ids = append(ids, updatedItem.ID)
					}
				}

				existingItems, err := dbp.SelectExistingPropsOfItemsByIDsAsc(m.Storage, ids, b.GetSource())
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
						it.URL = b.GetURL(it.ID)
					}
					if it.DiscussLink == "" {
						it.DiscussLink = b.GetDiscussLink(it.ID)
					}
					if it.Domain == "" {
						domain, err := util.URLToDomain(it.URL)
						if err == nil {
							it.Domain = domain
						}
					}
					if it.Source == "" {
						it.Source = b.GetSource()
					}
					if it.EncryptedURL == "" {
						link := it.URL
						if link == "" {
							link = it.DiscussLink
						}
						h, _ := encrypt.EncAndHex(link, m.Key)
						it.EncryptedURL = h
					}
					if it.EncryptedDiscussLink == "" {
						h, _ := encrypt.EncAndHex(it.DiscussLink, m.Key)
						it.EncryptedDiscussLink = h
					}
				}

				func() {
					m.Lock()
					defer m.Unlock()
					err = dbp.InsertOrReplaceItems(m.Storage, items)
					if err != nil {
						log.Println(err)
					}
				}()
			}
		}(pb)
	}

	wg.Wait()
}
