module Feature.Navigator exposing (Config, SavedItem, view)

import Domain.Collection exposing (Collection)
import Html exposing (Html, aside, button, details, div, form, h3, input, li, p, span, summary, text, ul)
import Html.Attributes exposing (attribute, class, classList, disabled, placeholder, title, type_, value)
import Html.Events exposing (onClick, onInput, onSubmit)
import Set exposing (Set)
import Ui.Icon
import Ui.Panel as Panel exposing (Presentation)


{-| A saved search row. `id` is the server id, or the query itself when
the on-device fallback list is active; `label` is the query to run.
-}
type alias SavedItem =
    { id : String
    , label : String
    }


type alias Config msg =
    { presentation : Presentation
    , current : String
    , saved : List SavedItem
    , savedStatus : Maybe String
    , savedSaving : Bool
    , localFallback : Bool
    , recent : List String
    , collections : List Collection
    , activeCollection : Maybe String
    , collectionBrowse : Maybe String
    , collectionStatus : Maybe String
    , collectionDraft : String
    , onCollectionDraft : String -> msg
    , onCreateCollection : String -> msg
    , onOpenCollection : String -> msg
    , onAllPosts : msg
    , onMoveCollectionPost : String -> Int -> msg
    , onRemoveCollectionPost : String -> msg
    , removingPosts : Set String
    , onRun : String -> msg
    , onSave : msg
    , onRemove : String -> msg
    , onClose : msg
    , writesEnabled : Bool
    }


view : Config msg -> Html msg
view config =
    let
        canSave =
            String.trim config.current /= "" && not (List.any (\item -> item.label == config.current) config.saved) && not config.savedSaving

        active =
            config.activeCollection
                |> Maybe.andThen (\id -> List.filter (\c -> c.id == id) config.collections |> List.head)
    in
    aside [ class (Panel.presentationClass "navigator" config.presentation), attribute "aria-label" "Library navigation" ]
        [ Panel.header "Library" config.presentation config.onClose
        , div [ class "panel-body" ]
            [ div [ class "nav-home" ]
                [ button
                    [ class "nav-item nav-library"
                    , classList [ ( "is-current", config.collectionBrowse == Nothing && config.current == "" ) ]
                    , type_ "button"
                    , onClick config.onAllPosts
                    ]
                    [ Ui.Icon.view "folder", span [] [ text "All posts" ] ]
                ]
            , div [ class "panel-section nav-section" ]
                [ div [ class "section-heading" ]
                    [ h3 [ class "panel-section-title" ] [ text "Saved searches" ]
                    , button [ class "nav-add", type_ "button", onClick config.onSave, disabled (not canSave), attribute "aria-label" "Save current search", title "Save current search" ] [ text "+" ]
                    ]
                , ul [ class "nav-list" ]
                    (List.map
                        (\item ->
                            li [ class "nav-row" ]
                                [ queryButton config item.label
                                , button
                                    [ class "nav-remove"
                                    , type_ "button"
                                    , onClick (config.onRemove item.id)
                                    , disabled config.savedSaving
                                    , attribute "aria-label" ("Remove saved search " ++ item.label)
                                    ]
                                    [ text "×" ]
                                ]
                        )
                        config.saved
                    )
                , case config.savedStatus of
                    Just status ->
                        p [ class "panel-note", attribute "role" "status" ] [ text status ]

                    Nothing ->
                        text ""
                , if config.localFallback && not (List.isEmpty config.saved) then
                    p [ class "panel-note" ] [ text "On this device" ]

                  else
                    text ""
                ]
            , if List.isEmpty config.recent then
                text ""

              else
                details [ class "panel-section nav-disclosure" ]
                    [ summary [] [ Ui.Icon.view "search", text "Recent searches" ]
                    , ul [ class "nav-list" ] (List.map (\query -> li [ class "nav-row" ] [ queryButton config query ]) config.recent)
                    ]
            , div [ class "panel-section nav-section" ]
                [ h3 [ class "panel-section-title" ] [ text "Collections" ]
                , ul [ class "nav-list" ]
                    (List.map
                        (\collection ->
                            li [ class "nav-row" ]
                                [ button
                                    [ class "nav-item nav-collection"
                                    , classList [ ( "is-current", Just collection.id == config.collectionBrowse ) ]
                                    , type_ "button"
                                    , onClick (config.onOpenCollection collection.id)
                                    ]
                                    [ Ui.Icon.view "folder"
                                    , span [ class "nav-name" ] [ text collection.name ]
                                    , span [ class "nav-count" ] [ text (String.fromInt (List.length collection.postIds)) ]
                                    ]
                                ]
                        )
                        config.collections
                    )
                , details [ class "nav-create" ]
                    [ summary [ title "Create a collection" ] [ text "+ New collection" ]
                    , form [ class "compact-form", onSubmit (config.onCreateCollection config.collectionDraft) ]
                        [ input [ class "nav-input", type_ "text", placeholder "Collection name", value config.collectionDraft, onInput config.onCollectionDraft, attribute "aria-label" "New collection name", disabled (not config.writesEnabled) ] []
                        , button [ class "button", type_ "submit", disabled (String.trim config.collectionDraft == "" || not config.writesEnabled) ] [ text "Create" ]
                        ]
                    ]
                , case config.collectionStatus of
                    Just status ->
                        p [ class "panel-note", attribute "role" "status" ] [ text status ]

                    Nothing ->
                        text ""
                , case active of
                    Nothing ->
                        text ""

                    Just collection ->
                        if List.isEmpty collection.postIds then
                            text ""

                        else
                            details [ class "collection-order" ]
                                [ summary [] [ text ("Manage " ++ collection.name) ]
                                , ul [ class "nav-list collection-order-list" ]
                                    (List.indexedMap
                                        (\idx postId ->
                                            li [ class "nav-row collection-order-row" ]
                                                [ span [ class "collection-order-id", title postId ] [ text ("#" ++ postId) ]
                                                , button [ class "collection-move", type_ "button", disabled (idx == 0 || not config.writesEnabled), onClick (config.onMoveCollectionPost postId -1), attribute "aria-label" ("Move #" ++ postId ++ " up") ] [ text "↑" ]
                                                , button [ class "collection-move", type_ "button", disabled (idx == List.length collection.postIds - 1 || not config.writesEnabled), onClick (config.onMoveCollectionPost postId 1), attribute "aria-label" ("Move #" ++ postId ++ " down") ] [ text "↓" ]
                                                , button [ class "collection-remove", type_ "button", disabled (not config.writesEnabled || Set.member postId config.removingPosts), onClick (config.onRemoveCollectionPost postId), attribute "aria-label" ("Remove #" ++ postId ++ " from collection") ] [ text "×" ]
                                                ]
                                        )
                                        collection.postIds
                                    )
                                ]
                ]
            ]
        ]


queryButton : Config msg -> String -> Html msg
queryButton config query =
    button
        [ class "nav-item"
        , classList [ ( "is-current", query == config.current ) ]
        , type_ "button"
        , title query
        , onClick (config.onRun query)
        ]
        [ Ui.Icon.view "search", span [ class "nav-name" ] [ text query ] ]
